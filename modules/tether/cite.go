package tether

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/tetherlink"
)

// Cite: the references this module resolves, through the gateway. Each is a read, and only a
// read: the client cite uses may call the tools in citeAllowed and nothing else, and each of
// those changes nothing. Tesseract items are read with tesseract_history (newest revision),
// not tesseract_get, which counts the read as a use of the item.
//
// Ported from Basalisk's resolver (internal/api/cite.go).

// citeAllowed are the only gateway tools a reference is looked up with. All read.
var citeAllowed = tetherlink.Allowed{
	"torque_task_get": true, "torque_epic_get": true, "torque_project_get": true,
	"torque_sprint_get": true, "torque_plan_get": true,
	"tesseract_history":      true,
	"tether_registry_lookup": true, "tether_message_trace": true,
	"caster_get": true,
}

// citeConcurrency bounds the gateway calls one batch makes at once.
const citeConcurrency = 6

var (
	ulidRe    = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)
	tessKeyRe = regexp.MustCompile(`^([a-z]{3,16}@)?[a-z][A-Za-z0-9_./-]{0,400}#[A-Za-z0-9_.:-]{1,160}$`)
	urnRe     = regexp.MustCompile(`^msg://[a-z][a-z0-9-]{0,31}/[A-Za-z0-9_.@:/-]{1,300}$`)
	uuidRe    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	casterRe  = regexp.MustCompile(`^caster://[A-Za-z0-9_.-]{1,128}/[A-Za-z0-9_.:-]{1,256}$`)
)

func torqueRe(prefix string) *regexp.Regexp {
	return regexp.MustCompile(`^` + prefix + `-\d{8}-\d{4}$`)
}

func (m *mod) refs() []module.RefKind {
	ready := func(_ context.Context, h module.Host) bool {
		if m.link != nil {
			return true
		}
		c := h.Config()
		return strings.TrimSpace(c.String("tether_command")) != "" || strings.TrimSpace(c.String("tether_url")) != ""
	}
	kind := func(id, label string, re *regexp.Regexp, one func(ctx context.Context, c tetherlink.Caller, id string, full bool) (module.RefEntity, error)) module.RefKind {
		return module.RefKind{Kind: id, Label: label, ID: re, Ready: ready,
			Resolve: func(ctx context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
				c, err := m.citeClient()
				if err != nil {
					return nil, err
				}
				return resolveEach(ctx, ids, func(ctx context.Context, id string) (module.RefEntity, error) { return one(ctx, c, id, false) })
			},
			Record: func(ctx context.Context, _ module.Host, id string) (module.RefEntity, error) {
				c, err := m.citeClient()
				if err != nil {
					return module.RefEntity{}, err
				}
				e, err := one(ctx, c, id, true)
				if notFound(err) {
					return e, module.ErrRefNotFound
				}
				return e, err
			}}
	}
	return []module.RefKind{
		kind("torque.task", "Torque task", torqueRe("CW"), torqueTask),
		kind("torque.epic", "Torque epic", torqueRe("EP"), torqueContainer("torque_epic_get", "Epic")),
		kind("torque.project", "Torque project", torqueRe("PRJ"), torqueContainer("torque_project_get", "Project")),
		kind("torque.sprint", "Torque sprint", torqueRe("SP"), torqueContainer("torque_sprint_get", "Sprint")),
		kind("torque.plan", "Torque plan", torqueRe("PL"), torqueContainer("torque_plan_get", "Plan")),
		kind("tesseract.item", "Tesseract item", ulidRe, tesseractItem),
		kind("tesseract.key", "Tesseract key", tessKeyRe, tesseractKey),
		kind("tether.agent", "Tether agent", urnRe, tetherAgent),
		kind("tether.message", "Tether message", uuidRe, tetherMessage),
		kind("caster.doc", "Caster document", casterRe, casterDoc),
	}
}

// citeClient is a client that may call the read tools in citeAllowed only.
func (m *mod) citeClient() (tetherlink.Caller, error) {
	if m.link != nil {
		return m.link, nil
	}
	cfg := m.h.Config()
	c := tetherlink.New("tether", tetherlink.Config{URL: cfg.String("tether_url"), TokenFile: cfg.String("tether_token_file"), Command: cfg.String("tether_command")}, citeAllowed)
	if c == nil {
		return nil, errNotConfigured
	}
	return c, nil
}

// resolveEach looks the ids up a few at a time. One that is not there is left out; when every
// lookup failed for another reason, that is the batch's error.
func resolveEach(ctx context.Context, ids []string, one func(context.Context, string) (module.RefEntity, error)) (map[string]module.RefEntity, error) {
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		out    = map[string]module.RefEntity{}
		failed error
		ok     int
		sem    = make(chan struct{}, citeConcurrency)
	)
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			e, err := one(ctx, id)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				out[id] = e
				ok++
			case notFound(err):
				ok++
			default:
				failed = err
			}
		}()
	}
	wg.Wait()
	if failed != nil && ok == 0 {
		return nil, failed
	}
	return out, nil
}

var errMissing = errors.New("not found")

// notFound reports whether a tool's failure says the record is not there (or may not be read,
// which the tools do not tell apart).
func notFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errMissing) || errors.Is(err, module.ErrRefNotFound) {
		return true
	}
	var r *tetherlink.RefusedError
	if errors.As(err, &r) {
		d := strings.ToLower(r.Detail)
		return strings.Contains(d, "not_found") || strings.Contains(d, "not found") || strings.Contains(d, "deleted") ||
			strings.Contains(d, "no such") || strings.Contains(d, "unknown")
	}
	return false
}

// ---- Projections ----

func excerpt(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		cut := strings.LastIndexAny(string(r[:n]), " \n")
		if cut < n/2 {
			cut = len(string(r[:n]))
		}
		return strings.TrimSpace(string(r[:n])[:cut]) + "…"
	}
	return s
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(strings.TrimLeft(s, "# "))
}

func addField(fields []module.RefField, label, value string, mono bool) []module.RefField {
	if strings.TrimSpace(value) == "" {
		return fields
	}
	return append(fields, module.RefField{Label: label, Value: value, Mono: mono})
}

func status(s string) *module.RefStatus {
	if s == "" {
		return nil
	}
	return &module.RefStatus{Label: s, Tone: module.StatusTone(s)}
}

// pick returns the first non-empty string among the keys, whichever case the tool used.
func pick(m map[string]any, keys ...string) string {
	for _, k := range keys {
		for _, name := range []string{k, strings.ToUpper(k[:1]) + k[1:]} {
			switch v := m[name].(type) {
			case string:
				if v != "" {
					return v
				}
			case map[string]any: // a sql.Null wrapper: {String, Valid}
				if s, _ := v["String"].(string); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func torqueTask(ctx context.Context, c tetherlink.Caller, id string, full bool) (module.RefEntity, error) {
	args := map[string]any{"id": id, "format": "typed", "comments": "false"}
	if full {
		args["comments"], args["comments_limit"] = "true", "25"
	}
	raw, err := c.Call(ctx, "torque_task_get", args)
	if err != nil {
		return module.RefEntity{}, err
	}
	var out struct {
		Data struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Status      string `json:"status"`
			Priority    int    `json:"priority"`
			Description string `json:"description"`
			ProjectID   string `json:"project_id"`
			EpicID      string `json:"epic_id"`
			SprintID    string `json:"sprint_id"`
			Kind        string `json:"kind"`
			UpdatedAt   string `json:"updated_at"`
			Tags        []struct {
				Name string `json:"name"`
			} `json:"tags"`
			Comments []struct {
				Author    string `json:"author"`
				Content   string `json:"content"`
				CreatedAt string `json:"created_at"`
			} `json:"comments"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out.Data.Title == "" {
		return module.RefEntity{}, errors.New("unreadable task")
	}
	t := out.Data
	tags := make([]string, len(t.Tags))
	for i, tag := range t.Tags {
		tags[i] = tag.Name
	}
	e := module.RefEntity{
		Title: t.Title, Subtitle: "Torque task", Description: excerpt(t.Description, 320),
		Status: status(t.Status), UpdatedAt: t.UpdatedAt, ChipLabel: "Task: " + t.Title,
	}
	e.Fields = addField(e.Fields, "Priority", fmt.Sprintf("P%d", t.Priority), false)
	e.Fields = addField(e.Fields, "Project", t.ProjectID, true)
	e.Fields = addField(e.Fields, "Epic", t.EpicID, true)
	e.Fields = addField(e.Fields, "Sprint", t.SprintID, true)
	e.Fields = addField(e.Fields, "Kind", t.Kind, false)
	e.Fields = addField(e.Fields, "Tags", strings.Join(tags, ", "), false)
	if full {
		var b strings.Builder
		b.WriteString(strings.TrimSpace(t.Description))
		if len(t.Comments) > 0 {
			b.WriteString("\n\n---\n\n## Comments\n")
			for _, cm := range t.Comments {
				fmt.Fprintf(&b, "\n**%s** · %s\n\n%s\n", cm.Author, cm.CreatedAt, strings.TrimSpace(cm.Content))
			}
		}
		e.Body = b.String()
	}
	return e, nil
}

func torqueContainer(tool, label string) func(context.Context, tetherlink.Caller, string, bool) (module.RefEntity, error) {
	return func(ctx context.Context, c tetherlink.Caller, id string, full bool) (module.RefEntity, error) {
		raw, err := c.Call(ctx, tool, map[string]any{"id": id})
		if err != nil {
			return module.RefEntity{}, err
		}
		var out struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil || out.Data == nil {
			return module.RefEntity{}, fmt.Errorf("unreadable %s", strings.ToLower(label))
		}
		title := pick(out.Data, "title", "name")
		if title == "" {
			title = id
		}
		description := pick(out.Data, "description", "goal", "summary")
		e := module.RefEntity{
			Title: title, Subtitle: "Torque " + strings.ToLower(label), Description: excerpt(description, 320),
			UpdatedAt: pick(out.Data, "updated_at", "updatedAt"), ChipLabel: label + ": " + title,
			Status: status(pick(out.Data, "status")),
		}
		e.Fields = addField(e.Fields, "Project", pick(out.Data, "project_id", "projectID"), true)
		if full {
			e.Body = description
		}
		return e, nil
	}
}

// tesseractRevision is one revision as tesseract_history answers it.
type tesseractRevision struct {
	ItemID    string   `json:"item_id"`
	MemoryID  string   `json:"memory_id"`
	Domain    string   `json:"domain"`
	Namespace string   `json:"namespace"`
	Key       string   `json:"key"`
	MemoryKey string   `json:"memory_key"`
	Status    string   `json:"status"`
	UpdatedAt string   `json:"updated_at"`
	CreatedAt string   `json:"created_at"`
	Tags      []string `json:"tags"`
	Author    struct {
		AgentID string `json:"agent_id"`
	} `json:"author"`
	Payload struct {
		Summary string `json:"summary"`
		Body    string `json:"body"`
	} `json:"payload"`
}

// newestRevision reads the newest revision out of a tesseract_history answer: a bare array, or
// {results, manifest} when a limit was given.
func newestRevision(raw string) (tesseractRevision, error) {
	var page struct {
		Results []tesseractRevision `json:"results"`
	}
	var list []tesseractRevision
	if err := json.Unmarshal([]byte(raw), &page); err == nil && len(page.Results) > 0 {
		list = page.Results
	} else if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return tesseractRevision{}, errors.New("unreadable Tesseract history")
	}
	if len(list) == 0 {
		return tesseractRevision{}, errMissing
	}
	return list[0], nil
}

// tesseractType names the kind of note from where it lives: ".../workspace/design" is a
// Design, ".../memory/handoffs" a Handoff.
func tesseractType(namespace string) string {
	seg := namespace
	if i := strings.LastIndexByte(namespace, '/'); i >= 0 {
		seg = namespace[i+1:]
	}
	seg = strings.TrimSuffix(seg, "s")
	if seg == "" {
		return "Note"
	}
	return strings.ToUpper(seg[:1]) + seg[1:]
}

func tesseractEntity(r tesseractRevision, full bool) module.RefEntity {
	key := r.Key
	if key == "" {
		key = r.MemoryKey
	}
	title := firstLine(r.Payload.Body)
	if title == "" || len(title) > 140 {
		title = excerpt(r.Payload.Summary, 140)
	}
	if title == "" {
		title = key
	}
	if title == "" {
		title = r.ItemID
	}
	updated := r.UpdatedAt
	if updated == "" {
		updated = r.CreatedAt
	}
	e := module.RefEntity{
		Title: title, Subtitle: strings.TrimSpace("Tesseract " + r.Domain), Description: excerpt(r.Payload.Summary, 320),
		UpdatedAt: updated, ChipLabel: tesseractType(r.Namespace) + ": " + title, Status: status(r.Status),
	}
	e.Fields = addField(e.Fields, "Namespace", r.Namespace, true)
	e.Fields = addField(e.Fields, "Key", key, true)
	e.Fields = addField(e.Fields, "Tags", strings.Join(r.Tags, ", "), false)
	e.Fields = addField(e.Fields, "Author", r.Author.AgentID, true)
	if full {
		e.Body = r.Payload.Body
		if e.Body == "" {
			e.Body = r.Payload.Summary
		}
	}
	return e
}

func tesseractItem(ctx context.Context, c tetherlink.Caller, id string, full bool) (module.RefEntity, error) {
	raw, err := c.Call(ctx, "tesseract_history", map[string]any{"item_id": id, "limit": 1})
	if err != nil {
		return module.RefEntity{}, err
	}
	r, err := newestRevision(raw)
	if err != nil {
		return module.RefEntity{}, err
	}
	return tesseractEntity(r, full), nil
}

// tesseractKey resolves "[domain@]namespace#key". Without a domain the revisioned ones are tried
// in turn (workspace drafts have no history, and reading them counts as a use).
func tesseractKey(ctx context.Context, c tetherlink.Caller, ref string, full bool) (module.RefEntity, error) {
	domains := []string{"knowledge", "memory"}
	if at := strings.IndexByte(ref, '@'); at > 0 && at < strings.IndexByte(ref, '#') {
		domains, ref = []string{ref[:at]}, ref[at+1:]
	}
	namespace, key, _ := strings.Cut(ref, "#")
	last := errMissing
	for _, domain := range domains {
		raw, err := c.Call(ctx, "tesseract_history", map[string]any{"domain": domain, "namespace": namespace, "key": key, "limit": 1})
		if err == nil {
			r, rerr := newestRevision(raw)
			if rerr == nil {
				return tesseractEntity(r, full), nil
			}
			last = rerr
			continue
		}
		last = err
		// A namespace that does not hold this domain answers a validation error: keep looking.
		var refused *tetherlink.RefusedError
		if !errors.As(err, &refused) {
			return module.RefEntity{}, err
		}
	}
	if !notFound(last) {
		var refused *tetherlink.RefusedError
		if errors.As(last, &refused) {
			return module.RefEntity{}, errMissing
		}
	}
	return module.RefEntity{}, last
}

func tetherAgent(ctx context.Context, c tetherlink.Caller, urn string, full bool) (module.RefEntity, error) {
	raw, err := c.Call(ctx, "tether_registry_lookup", map[string]any{"urn": urn})
	if err != nil {
		return module.RefEntity{}, err
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return module.RefEntity{}, errors.New("unreadable registry profile")
	}
	if inner, ok := p["profile"].(map[string]any); ok {
		p = inner
	}
	name := pick(p, "display_name", "name", "handle", "title")
	if name == "" {
		name = urn[strings.LastIndexByte(urn, '/')+1:]
	}
	e := module.RefEntity{
		Title: name, Subtitle: "Tether " + strings.TrimSpace(pick(p, "kind")), Description: excerpt(pick(p, "description", "about", "summary"), 320),
		ChipLabel: "Agent: " + name, Status: status(pick(p, "status")), UpdatedAt: pick(p, "updated_at"),
	}
	e.Fields = addField(e.Fields, "Handle", pick(p, "handle"), true)
	if full {
		e.Body = prettyJSON(p) // a JSON document: the page draws it as fields
	}
	return e, nil
}

func tetherMessage(ctx context.Context, c tetherlink.Caller, id string, full bool) (module.RefEntity, error) {
	raw, err := c.Call(ctx, "tether_message_trace", map[string]any{"message_id": id})
	if err != nil {
		return module.RefEntity{}, err
	}
	var t map[string]any
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return module.RefEntity{}, errors.New("unreadable message trace")
	}
	msg := t
	for _, k := range []string{"message", "envelope"} {
		if inner, ok := t[k].(map[string]any); ok {
			msg = inner
			break
		}
	}
	from, to := pick(msg, "from", "sender"), pick(msg, "to", "recipient")
	subject := pick(msg, "subject", "title", "kind")
	title := subject
	if title == "" {
		title = "Message " + id[:8]
	}
	e := module.RefEntity{
		Title: title, Subtitle: "Tether message", ChipLabel: "Message: " + title,
		Status: status(pick(msg, "state", "status")), UpdatedAt: pick(msg, "updated_at", "created_at"),
	}
	e.Fields = addField(e.Fields, "From", from, true)
	e.Fields = addField(e.Fields, "To", to, true)
	if full {
		// The delivery trace, as a JSON document (the message's text is read in its mailbox).
		e.Subtitle = "Tether message · delivery trace"
		e.Body = prettyJSON(t)
	}
	return e, nil
}

// casterMaxBytes bounds the text a Caster record is read with.
const casterMaxBytes = 256 << 10

// casterPreviewBytes is what a preview reads first: enough for a Markdown excerpt.
const casterPreviewBytes = 4096

// casterSummaryBytes is what a preview reads, once more, of a JSON document cut short at
// casterPreviewBytes, to summarize it. A document longer than this is not summarized.
const casterSummaryBytes = 64 << 10

type casterAnswer struct {
	Content   string `json:"content"`
	State     string `json:"state"`
	Revision  any    `json:"revision"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updated_at"`
	Truncated bool   `json:"truncated"`
}

func casterRead(ctx context.Context, c tetherlink.Caller, ref string, limit int) (casterAnswer, error) {
	raw, err := c.Call(ctx, "caster_get", map[string]any{"ref": ref, "max_bytes": limit})
	if err != nil {
		return casterAnswer{}, err
	}
	var d casterAnswer
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return casterAnswer{}, errors.New("unreadable Caster document")
	}
	return d, nil
}

// casterDoc projects a Caster document. Its content is Markdown (front matter left out of the
// preview) or a JSON document: then the preview is a summary of it (summaryOfJSON), never a
// fragment of the text, and the record's Body is the JSON itself, which the page draws as
// fields (module.RefEntity.Body).
func casterDoc(ctx context.Context, c tetherlink.Caller, ref string, full bool) (module.RefEntity, error) {
	limit := casterPreviewBytes
	if full {
		limit = casterMaxBytes
	}
	d, err := casterRead(ctx, c, ref, limit)
	if err != nil {
		return module.RefEntity{}, err
	}
	// The text past any front matter (a collab/v1 document has one) is Markdown or JSON.
	text := stripFrontMatter(d.Content)
	if !full && d.Truncated && looksLikeJSON(text) {
		if more, err := casterRead(ctx, c, ref, casterSummaryBytes); err == nil {
			d, text = more, stripFrontMatter(more.Content)
		}
	}
	doc, isJSON := parseJSONDoc(text)
	// JSON that does not parse because it was cut short; text that only starts like JSON
	// ("[Spec](…)", "[ ] a task") and was not cut is Markdown.
	cutJSON := !isJSON && d.Truncated && looksLikeJSON(text)
	title := strings.TrimSpace(d.Title)
	if title == "" && isJSON {
		if m, ok := doc.(map[string]any); ok {
			title = pick(m, "title", "name", "subject")
		}
	}
	if title == "" && !isJSON && !cutJSON {
		title = firstLine(text)
	}
	if title == "" {
		title = ref[strings.LastIndexByte(ref, '/')+1:]
	}
	e := module.RefEntity{
		Title: title, Subtitle: "Caster document", ChipLabel: "Caster: " + title,
		Status: status(d.State), UpdatedAt: d.UpdatedAt, Open: casterOpen(ref),
	}
	switch {
	case isJSON:
		e.Description = summaryOfJSON(doc)
	case cutJSON:
		e.Description = "A JSON document, too long to summarize here."
	default:
		e.Description = excerpt(text, 320)
	}
	if d.Revision != nil {
		e.Fields = addField(e.Fields, "Revision", fmt.Sprint(d.Revision), false)
	}
	if isJSON || cutJSON {
		e.Fields = addField(e.Fields, "Format", "JSON", false)
	}
	if full {
		switch {
		case isJSON:
			e.Body = strings.TrimSpace(d.Content) // front matter and all: the page lays out both
		case cutJSON: // no longer JSON, so shown as code
			e.Body = "```json\n" + strings.TrimRight(text, "\n") + "\n```\n\n…(cut short)"
		default:
			e.Body = d.Content
			if d.Truncated {
				e.Body += "\n\n…(cut short)"
			}
		}
	}
	return e, nil
}

// casterOpen is the module that shows a Caster document as its own, by its namespace: a collab
// diagram in Diagram, a collab draft in Drafts, Work's index ("work") and workspace namespaces
// ("w-<slug>") in Work (work.open-ref). The page runs the command when the app has it and falls
// back to the record window when it fails; scratch items and anything else with no window of
// its own are shown in the record window.
func casterOpen(ref string) *module.RefOpen {
	rest := strings.TrimPrefix(ref, "caster://")
	rest, _, _ = strings.Cut(rest, "@")
	ns, name, _ := strings.Cut(rest, "/")
	switch ns {
	case "collab":
		switch {
		case strings.HasPrefix(name, "diagram.") && !strings.HasSuffix(name, ".marks"):
			return &module.RefOpen{Command: "diagram.open", Args: map[string]any{"id": name}}
		case strings.HasPrefix(name, "draft.") && !strings.HasSuffix(name, ".marks"):
			return &module.RefOpen{Command: "drafts.open", Args: map[string]any{"id": name}}
		}
		return nil
	case "work":
		return &module.RefOpen{Command: "work.open-ref", Args: map[string]any{"ref": ref}}
	}
	if strings.HasPrefix(ns, "w-") { // a Work workspace's own namespace
		return &module.RefOpen{Command: "work.open-ref", Args: map[string]any{"ref": ref}}
	}
	return nil
}

func stripFrontMatter(s string) string {
	t := strings.TrimLeft(s, "\n")
	if !strings.HasPrefix(t, "---\n") {
		return s
	}
	if end := strings.Index(t[4:], "\n---"); end >= 0 {
		return strings.TrimSpace(t[4+end+4:])
	}
	return s
}

func prettyJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return clip(string(b), 20000)
}
