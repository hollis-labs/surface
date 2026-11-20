// Package hostrunner is a runner for the conversation wire (libs agent/chat) backed by
// libs agent/host: Basalisk's agent runtime, extracted. It gives the conversation wire the
// host's way with context:
//
//   - The system prompt is fixed. The turn's context goes in front of the owner's message,
//     in the same stored message (host.Config.DescribeContext), so the history the model is
//     sent stays the same from one request to the next and the provider's cache holds.
//   - When the last request of a thread passed CompactAt tokens, the older part of the
//     thread is summarized once and the model is sent the summary, then the recent turns in
//     full. Nothing is deleted.
//
// One host thread serves one chat conversation: the host keeps one running thread per
// owner, and the runner's owner for a conversation is OwnerKey(conversationID). The
// threads, their messages, summaries and runs are the host's own tables (agenthost_*) in the
// database the runner is given, so a restart resumes the same thread.
//
// The host is driven through its exported surface only. Its run events are read from its
// own handler, in process. Its usage events do not tell cached input from uncached, so the
// model gateway is reached through a loopback tap (tap.go) that reads the usage the
// gateway reports and hands it to the run. Approvals are the host's own approval.request,
// answered through the chat wire; whether a call asks is decided by the levels, the host
// application's Ask and its standing grants (Granted), through the host's store.
package hostrunner

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	chat "github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/libs/agent/host"
	"github.com/hollis-labs/libs/llm/core/llmtypes"
)

// Provider is the provider a run's run.start names.
const Provider = "agenthost"

// ActivityCompact reports compaction of the conversation's thread. Value: state ("start",
// "done" or "failed"), before (the size of the thread's last request, in tokens) and,
// when known, after (the size of the first request sent after it).
const ActivityCompact = "host.compact"

// DefaultCompactAt is Basalisk's: a thread whose last request passed 110,000 tokens is
// compacted before the next.
const DefaultCompactAt = 110_000

// toolLongest bounds one call of an application tool. The host gives its server tools 90
// seconds; an application tool that waits for the owner (a card that asks) needs longer,
// so the runner runs it on its own clock, stopped with the run.
const toolLongest = 70 * time.Minute

// Levels, as the host and the chat wire name them.
const (
	LevelRead    = host.LevelRead
	LevelAct     = host.LevelAct
	LevelWrite   = host.LevelWrite
	LevelConfirm = host.LevelConfirm
)

// Tool is one tool of the application's that the agent may call.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	// Level is read, act, write or confirm. Empty or unknown is write.
	Level string
	// Run returns what the model is told. An error is told to the model as a failure.
	Run func(ctx context.Context, input json.RawMessage) (string, error)
}

// Call identifies a tool call for Ask and Granted.
type Call struct {
	ConversationID string
	Tool           string
}

// Options configures a Runner.
type Options struct {
	// Config is the host's: Model, BaseURL, KeyFile, System, MaxTokens, MaxSteps, CompactAt.
	// DescribeContext, SessionHeader and Provider are the runner's and are replaced.
	Config host.Config
	// DB keeps the host's tables (agenthost_*).
	DB *sql.DB
	// Tools are the application's tools.
	Tools []Tool
	// Ask is called before a call of a tool that would run without asking (read or act).
	// True makes it ask. It can only make a call stricter.
	Ask func(ctx context.Context, call Call) bool
	// Granted reports a standing grant (one the host application keeps through
	// chat.Grants) that covers a call of a write tool. It is not consulted for confirm.
	Granted func(ctx context.Context, call Call) bool
	// Seed is the text put ahead of the context on the first turn of a conversation's
	// thread ("" for none): what the agent should know about the conversation before it.
	Seed   func(ctx context.Context, conversationID string) string
	Logger *slog.Logger
}

// Runner is a chat.Runner backed by libs agent/host.
type Runner struct {
	opts    Options
	h       *host.Host
	store   *host.SQLite
	handler http.Handler
	tap     *tap
	levels  map[string]string
	log     *slog.Logger

	// ctx ends when the runner closes: every tool call is stopped with it.
	ctx  context.Context
	stop context.CancelFunc

	replaced chan string

	mu       sync.Mutex
	active   map[string]*run   // by owner key
	byThread map[string]*run   // by host thread
	threads  map[string]string // conversation -> thread
}

var _ chat.Runner = (*Runner)(nil)

// ownerHeader carries the owner key to the host's handler, which the runner alone calls.
const ownerHeader = "X-Hostrunner-Owner"

// threadHeader carries the thread from the host's gateway to the tap.
const threadHeader = "X-Hostrunner-Thread"

// handlerPrefix is where the runner mounts the host's handler for itself.
const handlerPrefix = "/host"

// Open builds the runner. Close it when the application stops.
func Open(ctx context.Context, opts Options) (*Runner, error) {
	c := opts.Config
	if c.Model == "" || c.BaseURL == "" || c.KeyFile == "" {
		return nil, errors.New("hostrunner: the host runner needs a model, a gateway URL and a key file")
	}
	if opts.DB == nil {
		return nil, errors.New("hostrunner: no database")
	}
	if c.CompactAt == 0 {
		c.CompactAt = DefaultCompactAt
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	st, err := host.NewSQLite(ctx, opts.DB)
	if err != nil {
		return nil, fmt.Errorf("hostrunner: store: %w", err)
	}
	rctx, stop := context.WithCancel(context.Background())
	r := &Runner{opts: opts, store: st, levels: map[string]string{}, log: log, ctx: rctx, stop: stop,
		replaced: make(chan string), active: map[string]*run{}, byThread: map[string]*run{}, threads: map[string]string{}}
	r.tap, err = openTap(c.BaseURL, r.step, log)
	if err != nil {
		stop()
		return nil, err
	}
	c.BaseURL = r.tap.url
	c.SessionHeader = threadHeader
	c.DescribeContext = contextText
	c.Provider = nil
	r.h = host.New(c, store{SQLite: st, r: r})
	if err := r.h.AddServerTools(r.serverTools(opts.Tools)...); err != nil {
		r.h.Close()
		r.tap.close()
		stop()
		return nil, fmt.Errorf("hostrunner: %w", err)
	}
	r.handler = r.h.Handler(handlerPrefix, func(req *http.Request) (string, error) {
		if o := req.Header.Get(ownerHeader); o != "" {
			return o, nil
		}
		return "", errors.New("no owner")
	})
	return r, nil
}

// Close stops every run and the tap. It does not close the database.
func (r *Runner) Close() error {
	r.stop()
	r.h.Close()
	r.tap.close()
	return nil
}

// Config is the host's configuration in use.
func (r *Runner) Config() host.Config { return r.h.Config() }

// OwnerKey is the host owner whose running thread serves a chat conversation.
func OwnerKey(conversationID string) string { return "chat:" + conversationID }

func conversationOf(owner string) string { return strings.TrimPrefix(owner, "chat:") }

// Thread is the host thread that serves a conversation, created on first use.
func (r *Runner) Thread(ctx context.Context, conversationID string) (string, error) {
	r.mu.Lock()
	id := r.threads[conversationID]
	r.mu.Unlock()
	if id != "" {
		return id, nil
	}
	id, err := r.store.MainThread(ctx, OwnerKey(conversationID))
	if err != nil {
		return "", fmt.Errorf("hostrunner: thread: %w", err)
	}
	r.mu.Lock()
	r.threads[conversationID] = id
	r.mu.Unlock()
	return id, nil
}

// History is the conversation's thread as the host keeps it, oldest first.
func (r *Runner) History(ctx context.Context, conversationID string) ([]host.Message, error) {
	thread, err := r.Thread(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	return r.store.Messages(ctx, thread)
}

// Summary is the conversation's compaction summary and the last message it covers.
func (r *Runner) Summary(ctx context.Context, conversationID string) (string, int, error) {
	thread, err := r.Thread(ctx, conversationID)
	if err != nil {
		return "", 0, err
	}
	return r.store.Summary(ctx, thread)
}

// Info reports what the runner does and the thread that serves a conversation.
func (r *Runner) Info(ctx context.Context, conversationID string) (chat.Info, error) {
	thread, err := r.Thread(ctx, conversationID)
	if err != nil {
		return chat.Info{}, err
	}
	return chat.Info{Deltas: true, Approvals: true, RunnerRef: thread}, nil
}

// Replaced never sends: a host thread is not replaced behind the Service's back. A restart
// resumes the same thread; the host application signals its own start.
func (r *Runner) Replaced() <-chan string { return r.replaced }

// Record adds an exchange the host application answered itself to the thread.
func (r *Runner) Record(ctx context.Context, turn chat.Turn, answer string) error {
	text, answer := strings.TrimSpace(turn.Text), strings.TrimSpace(answer)
	if text == "" || answer == "" {
		return nil
	}
	thread, err := r.Thread(ctx, turn.ConversationID)
	if err != nil {
		return err
	}
	blocks := []llmtypes.ContentBlock{}
	if seed := r.seed(ctx, turn.ConversationID, thread); seed != "" {
		blocks = append(blocks, llmtypes.ContentBlock{Type: "text", Text: seed})
	}
	blocks = append(blocks, llmtypes.ContentBlock{Type: "text", Text: text})
	meta, _ := json.Marshal(map[string]any{"text": text, "answered_by": "host"})
	if _, err = r.store.Append(ctx, thread, "", "user", blocks, meta); err != nil {
		return err
	}
	_, err = r.store.Append(ctx, thread, "", "assistant", []llmtypes.ContentBlock{{Type: "text", Text: answer}}, nil)
	return err
}

// seed is the text that leads the first turn of a thread, or "".
func (r *Runner) seed(ctx context.Context, conversationID, thread string) string {
	if r.opts.Seed == nil {
		return ""
	}
	msgs, err := r.store.Messages(ctx, thread)
	if err != nil || len(msgs) > 0 {
		return ""
	}
	return strings.TrimSpace(r.opts.Seed(ctx, conversationID))
}

// contextText is the turn's context as the model sees it, ahead of the message: a JSON
// string's value, or any other JSON as written.
func contextText(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == `""` {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	return trimmed
}

// autoLabel is the host's auto label for a turn the owner did not type: the module of an
// errand, or the turn's own auto label.
func autoLabel(meta json.RawMessage) string {
	var m struct {
		Auto   any    `json:"auto"`
		Errand string `json:"errand"`
	}
	if len(meta) == 0 || json.Unmarshal(meta, &m) != nil {
		return ""
	}
	if m.Errand != "" {
		return "errand:" + m.Errand
	}
	switch v := m.Auto.(type) {
	case string:
		return v
	case bool:
		if v {
			return "auto"
		}
	}
	return ""
}

// Start begins a turn on the conversation's thread.
func (r *Runner) Start(ctx context.Context, turn chat.Turn) (chat.Run, error) {
	owner := OwnerKey(turn.ConversationID)
	thread, err := r.Thread(ctx, turn.ConversationID)
	if err != nil {
		return nil, err
	}
	text := contextText(turn.Context)
	if seed := r.seed(ctx, turn.ConversationID, thread); seed != "" {
		text = strings.TrimSpace(seed + "\n\n" + text)
	}
	var raw json.RawMessage
	if text != "" {
		raw, _ = json.Marshal(text)
	}
	before, _ := r.store.LastPrefix(ctx, thread)
	x := newRun(r, turn, owner, thread, before)

	r.mu.Lock()
	if r.active[owner] != nil {
		r.mu.Unlock()
		x.stop()
		return nil, chat.ErrBusy
	}
	r.active[owner], r.byThread[thread] = x, x
	r.mu.Unlock()

	id, err := r.h.Start(ctx, host.Turn{Owner: owner, Text: turn.Text, Context: raw, Auto: autoLabel(turn.Meta)})
	if err != nil {
		r.release(x)
		x.stop()
		if errors.Is(err, host.ErrBusy) {
			return nil, chat.ErrBusy
		}
		return nil, fmt.Errorf("hostrunner: %w", err)
	}
	x.hostRun = id
	return x, nil
}

func (r *Runner) release(x *run) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active[x.owner] == x {
		delete(r.active, x.owner)
	}
	if r.byThread[x.thread] == x {
		delete(r.byThread, x.thread)
	}
}

func (r *Runner) activeRun(owner string) *run {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active[owner]
}

// step is the tap asking where the usage of one model request on a thread goes.
func (r *Runner) step(thread string) *usageStep {
	r.mu.Lock()
	x := r.byThread[thread]
	r.mu.Unlock()
	if x == nil {
		return &usageStep{}
	}
	return x.newStep()
}

// level is a tool's level as the application gave it.
func (r *Runner) level(tool string) string { return r.levels[tool] }

func normalLevel(l string) string {
	switch l {
	case LevelRead, LevelAct, LevelWrite, LevelConfirm:
		return l
	}
	return LevelWrite
}

// serverTools are the application's tools as the host's server tools.
//
// The host asks before a write call unless the owner allowed the tool for the session or
// its store says the tool is granted, and before every confirm call. So every tool is given
// to the host as write (or confirm), and the store's Granted (store below) decides what a
// call of a read or act tool, or of a tool the application asks about first, does.
func (r *Runner) serverTools(tools []Tool) []host.ServerTool {
	out := make([]host.ServerTool, 0, len(tools))
	seen := map[string]bool{}
	for _, t := range tools {
		if seen[t.Name] || t.Run == nil {
			continue
		}
		seen[t.Name] = true
		lvl := normalLevel(t.Level)
		r.levels[t.Name] = lvl
		hostLevel := LevelWrite
		if lvl == LevelConfirm {
			hostLevel = LevelConfirm
		}
		run := t.Run
		out = append(out, host.ServerTool{
			Spec: host.ToolSpec{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema, Level: hostLevel},
			Run: func(toolCtx context.Context, owner string, input map[string]any) (string, error) {
				raw, err := json.Marshal(input)
				if err != nil {
					return "", err
				}
				// The host's 90-second bound is for quick server tools; this call runs on the
				// runner's clock, stopped with its run or the runner.
				ctx, cancel := context.WithTimeout(context.WithoutCancel(toolCtx), toolLongest)
				defer cancel()
				defer context.AfterFunc(r.ctx, cancel)()
				defer context.AfterFunc(toolCtx, func() {
					if !errors.Is(toolCtx.Err(), context.DeadlineExceeded) {
						cancel() // the run stopped, not the host's clock
					}
				})()
				if x := r.activeRun(owner); x != nil {
					defer context.AfterFunc(x.ctx, cancel)()
					ctx = context.WithValue(ctx, turnKey{}, x.turn)
				}
				return run(ctx, raw)
			},
		})
	}
	return out
}

type turnKey struct{}

// TurnFromContext is the chat turn a tool is running for.
func TurnFromContext(ctx context.Context) (chat.Turn, bool) {
	t, ok := ctx.Value(turnKey{}).(chat.Turn)
	return t, ok
}

// store is the host's store with the application's approvals: the host asks the store
// whether a tool is granted before it asks the owner.
type store struct {
	*host.SQLite
	r *Runner
}

// Granted decides whether a call asks: a read or act tool runs unless the application
// asks first; a write tool runs when a standing grant covers it.
func (s store) Granted(ctx context.Context, owner, tool string) (bool, error) {
	r := s.r
	call := Call{ConversationID: conversationOf(owner), Tool: tool}
	switch r.level(tool) {
	case LevelConfirm:
		return false, nil
	case LevelRead, LevelAct:
		if r.opts.Ask == nil || !r.opts.Ask(ctx, call) {
			return true, nil
		}
	}
	return r.opts.Granted != nil && r.opts.Granted(ctx, call), nil
}

// Grant keeps nothing: an "always" answer is kept by the chat Service in the host
// application's grants before the runner is answered.
func (store) Grant(context.Context, string, string) error { return nil }
