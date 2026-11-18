package design

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Message is mail to the Design team.
type Message struct {
	From, To string
	// Kind is a Tether message kind: "request" for new work and feedback, "notice" otherwise.
	Kind    string
	Subject string
	Body    string
	// Thread groups a job's messages: the job's id.
	Thread string
}

// Task is the Torque task that tracks a job. It is created manual: nothing starts on its own.
type Task struct {
	Project     string
	Title       string
	Description string
}

// Dispatcher reaches the Design team: out by Tether mail and a Torque task. The way back is
// Reader, which the same transport implements. Both are interfaces so the transport can be
// replaced without touching the module.
type Dispatcher interface {
	Send(ctx context.Context, m Message) (id string, err error)
	CreateTask(ctx context.Context, t Task) (id string, err error)
}

// Inbound is one message read from the sender's mailbox.
type Inbound struct {
	ID, From, To, Thread, Subject, Body, Sent string
}

// TaskState is what the Torque task says: its status and its latest comment.
type TaskState struct {
	Status        string
	Comment       string
	CommentAuthor string
	CommentAt     string
}

// Reader is the way back: the replies waiting on a job's thread, and the job's task. A
// Dispatcher that is also a Reader lets the module follow a job without anyone setting it by
// hand. Unread lists only one thread, so mail on any other thread is never fetched or touched.
type Reader interface {
	Unread(ctx context.Context, to, thread string) ([]Inbound, error)
	MarkRead(ctx context.Context, as, id string) error
	Task(ctx context.Context, id string) (TaskState, error)
}

// errNotConfigured means nothing to send with: the text is handed back to send by hand.
var errNotConfigured = errors.New("tether is not configured for the Design module (settings: design.tether_url or design.tether_command, design.sender_urn, design.designer_urn)")

// mcpDispatcher calls Tether's own tools, tether_message_send and torque_task_create.
type mcpDispatcher struct{ mcp mcpCaller }

var idPattern = regexp.MustCompile(`"(?:id|ID|message_id|MessageID)"\s*:\s*"([^"]+)"`)

// firstID finds an id in a tool's answer, whatever its exact shape.
func firstID(text string) string {
	if m := idPattern.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

func (d mcpDispatcher) Send(ctx context.Context, m Message) (string, error) {
	payload, _ := json.Marshal(map[string]string{"subject": m.Subject, "body": m.Body})
	out, err := d.mcp.Call(ctx, "tether_message_send", map[string]any{
		"from": m.From, "to": m.To, "kind": m.Kind, "thread_id": m.Thread, "payload_json": string(payload),
	})
	if err != nil {
		return "", err
	}
	return firstID(out), nil
}

func (d mcpDispatcher) CreateTask(ctx context.Context, t Task) (string, error) {
	out, err := d.mcp.Call(ctx, "torque_task_create", map[string]any{
		"title": t.Title, "description": t.Description, "project_id": t.Project,
		"manual": true, "source_type": "api", "tags": `["design","surface"]`,
	})
	if err != nil {
		return "", err
	}
	return firstID(out), nil
}

func (d mcpDispatcher) Unread(ctx context.Context, to, thread string) ([]Inbound, error) {
	out, err := d.mcp.Call(ctx, "tether_message_list", map[string]any{"to": to, "thread_id": thread, "unread_only": true, "limit": 100})
	if err != nil {
		return nil, err
	}
	var page struct {
		Messages []struct {
			ID        string `json:"id"`
			From      string `json:"from"`
			To        string `json:"to"`
			ThreadID  string `json:"thread_id"`
			Subject   string `json:"subject"`
			Body      string `json:"body"`
			CreatedAt string `json:"created_at"`
			ReadAt    any    `json:"read_at"`
			Payload   struct {
				Subject string `json:"subject"`
				Body    string `json:"body"`
			} `json:"payload"`
		} `json:"messages"`
	}
	if err := decodeResult(out, &page); err != nil {
		return nil, errors.New("tether: the message list could not be read")
	}
	msgs := make([]Inbound, 0, len(page.Messages))
	for _, m := range page.Messages {
		if m.ReadAt != nil && m.ReadAt != "" {
			continue // read already, whatever the filter did
		}
		msgs = append(msgs, Inbound{ID: m.ID, From: m.From, To: m.To, Thread: m.ThreadID,
			Subject: or(m.Payload.Subject, m.Subject), Body: or(m.Payload.Body, m.Body), Sent: m.CreatedAt})
	}
	return msgs, nil
}

func (d mcpDispatcher) MarkRead(ctx context.Context, as, id string) error {
	_, err := d.mcp.Call(ctx, "tether_message_mark_read", map[string]any{"message_id": id, "as": as})
	return err
}

func (d mcpDispatcher) Task(ctx context.Context, id string) (TaskState, error) {
	out, err := d.mcp.Call(ctx, "torque_task_get", map[string]any{"id": id, "comments_limit": "1"})
	if err != nil {
		return TaskState{}, err
	}
	var rec struct {
		Data struct {
			Status   string `json:"Status"`
			Comments []struct {
				Author    string `json:"author"`
				Content   string `json:"content"`
				CreatedAt string `json:"created_at"`
			} `json:"Comments"`
		} `json:"data"`
	}
	if err := decodeResult(out, &rec); err != nil || rec.Data.Status == "" {
		return TaskState{}, errors.New("torque: the task could not be read")
	}
	ts := TaskState{Status: rec.Data.Status}
	if n := len(rec.Data.Comments); n > 0 {
		c := rec.Data.Comments[n-1] // oldest to newest: the last is the latest
		ts.Comment, ts.CommentAuthor, ts.CommentAt = c.Content, c.Author, c.CreatedAt
	}
	return ts, nil
}

// decodeResult reads a tool's JSON answer, also when a proxy hands it on as a JSON string.
func decodeResult(text string, v any) error {
	err := json.Unmarshal([]byte(text), v)
	if err == nil {
		return nil
	}
	var inner string
	if json.Unmarshal([]byte(text), &inner) == nil {
		return json.Unmarshal([]byte(inner), v)
	}
	return err
}

// dispatcher builds the Dispatcher the settings describe, or reports what is missing.
func (m *mod) dispatcher() (Dispatcher, error) {
	if m.dispatch != nil {
		return m.dispatch, nil
	}
	cfg := m.h.Config()
	if cfg.String("sender_urn") == "" || cfg.String("designer_urn") == "" {
		return nil, errNotConfigured
	}
	if u := cfg.String("tether_url"); u != "" {
		return mcpDispatcher{&httpMCP{url: u, tokenFile: cfg.String("tether_token_file"), client: &http.Client{Timeout: 20 * time.Second}}}, nil
	}
	if c := strings.Fields(cfg.String("tether_command")); len(c) > 0 {
		return mcpDispatcher{&stdioMCP{argv: c, timeout: 30 * time.Second}}, nil
	}
	return nil, errNotConfigured
}

// Outbox is what was (or should be) sent for one ask, as the tools report it.
type Outbox struct {
	Sent      bool   `json:"sent"`
	MessageID string `json:"message_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	// Problem is why it was not sent, in a line; Manual is then the text to send by hand.
	Problem string `json:"problem,omitempty"`
	Manual  string `json:"manual,omitempty"`
}

// deliver sends a message, and a task when one is given. Whatever fails, the job stands and
// the text comes back to be sent by hand.
func (m *mod) deliver(ctx context.Context, msg Message, task *Task) Outbox {
	cfg := m.h.Config()
	msg.From, msg.To = cfg.String("sender_urn"), cfg.String("designer_urn")
	manual := func(problem string) Outbox {
		var b strings.Builder
		fmt.Fprintf(&b, "To: %s\nSubject: %s\n\n%s", or(msg.To, "the Designer"), msg.Subject, msg.Body)
		if task != nil {
			fmt.Fprintf(&b, "\n\n---\nTorque task (manual) in project %s:\n%s\n\n%s", or(task.Project, "(the design project)"), task.Title, task.Description)
		}
		return Outbox{Problem: problem, Manual: b.String()}
	}
	d, err := m.dispatcher()
	if err != nil {
		return manual(err.Error())
	}
	out := Outbox{}
	if task != nil {
		if task.Project == "" {
			return manual("no Torque project is set (design.torque_project)")
		}
		id, terr := d.CreateTask(ctx, *task)
		if terr != nil {
			return manual("the Torque task could not be created: " + terr.Error())
		}
		out.TaskID = id
		msg.Body += "\n\nTracked as Torque task " + or(id, "(id not returned)") + "."
	}
	id, err := d.Send(ctx, msg)
	if err != nil {
		o := manual("the Tether message could not be sent: " + err.Error())
		o.TaskID = out.TaskID
		return o
	}
	out.Sent, out.MessageID = true, id
	return out
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// The texts the Design team receives.

func (m *mod) briefMessage(j Job) (Message, Task) {
	repo := m.h.Config().String("repo")
	base := m.h.Config().String("base_branch")
	var b strings.Builder
	fmt.Fprintf(&b, "Design job %s: %s\n\n%s\n", j.ID, j.Title, j.Brief)
	if j.Refs != "" {
		fmt.Fprintf(&b, "\nReferences:\n%s\n", j.Refs)
	}
	fmt.Fprintf(&b, `
How to work it:
- Repository %s. Branch from %s as %s and open a pull request from it.
- Follow docs/gui.md in that repository: a look is changed in registry/ui or registry/theme, never in an app; every component and variant gets its specimen in web/src/gallery.
- Serve the gallery from your checkout with hot reload so it can be reviewed live: make design-preview HOST=<address the reviewer can reach> PORT=<port>. It prints the URL.
- Review notes come back as a new round on this thread. Merge the pull request yourselves once it is approved here; Surface does not merge.
%s`, or(repo, "the Surface repository"), or(base, "main"), j.Branch, replyHow(j, base))
	msg := Message{Kind: "request", Subject: "Design job " + j.ID + ": " + j.Title, Body: b.String(), Thread: "design-" + j.ID}
	task := Task{
		Project:     m.h.Config().String("torque_project"),
		Title:       "Design: " + j.Title + " (" + j.ID + ")",
		Description: b.String(),
	}
	return msg, task
}

func feedbackMessage(j Job, round int, text string, notes []noteLine) Message {
	var b strings.Builder
	fmt.Fprintf(&b, "Design job %s, round %d: changes requested on %s.\n", j.ID, round, j.Branch)
	if t := strings.TrimSpace(text); t != "" {
		fmt.Fprintf(&b, "\n%s\n", t)
	}
	if len(notes) > 0 {
		b.WriteString("\nNotes pinned in the review:\n")
		for _, n := range notes {
			fmt.Fprintf(&b, "- [%s] %s\n", n.Where, n.Body)
		}
	}
	b.WriteString("\nPush to the same branch; the preview updates as you do.\n")
	b.WriteString(replyHow(j, ""))
	return Message{Kind: "request", Subject: fmt.Sprintf("Design job %s: round %d", j.ID, round), Body: b.String(), Thread: "design-" + j.ID}
}

// replyHow is how a reply is read: free text, plus labeled lines that set the job's fields.
// It travels with every ask, so the contract is in front of whoever answers.
func replyHow(j Job, base string) string {
	return fmt.Sprintf(`
Reply on this thread (design-%s), to this address. Say what you like in the text; put what should be set on the job on lines of their own, each optional, a later reply replacing an earlier value:
preview: <URL of the gallery served from %s>
base: <URL of the same from %s, for comparison>
pr: <URL of the pull request>
head: <the full commit SHA the preview shows>
An approval names one head (approved-head:). Merge that commit only, never a later one: a later commit needs a new round. Nothing can be approved until a reply names the head.
`, j.ID, j.Branch, or(base, "the base branch"))
}

// approvedMessage is the go-ahead. Its labeled lines name exactly what was approved, so the
// team can check the pull request's head against them before merging.
func approvedMessage(j Job) Message {
	var b strings.Builder
	fmt.Fprintf(&b, "Design job %s (%s) is approved at commit %s.\n\n", j.ID, j.Title, j.ApprovedHead)
	fmt.Fprintf(&b, "job: %s\napproved-head: %s\n", j.ID, j.ApprovedHead)
	fmt.Fprintf(&b, "pr: %s\n", j.ApprovedPR)
	fmt.Fprintf(&b, "\nThis approval covers pull request %s at commit %s only. Merge that pull request, and only if its head is exactly that commit; another pull request, or a later commit, is not approved and needs a new round. When it is in, reply with a line merged: <the full SHA you merged> (or closed: <why> if it will not be merged).\n", j.ApprovedPR, j.ApprovedHead)
	return Message{Kind: "notice", Subject: "Design job " + j.ID + ": approved at " + short(j.ApprovedHead) + ", merge that commit", Body: b.String(), Thread: "design-" + j.ID}
}
