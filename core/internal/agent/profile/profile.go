// Package profile reads, checks and stages module agent profiles.
//
// A module may ship a profile (module.Manifest.Profile): a Nanite agent directory without
// host.yaml or .mcp.json, kept in modules/<id>/profile/ and embedded. While the module is in
// front, or after an explicit switch, the app's one agent works as that profile. The app
// owns the runtime, the model, the identity, MCP and permissions; every profile shares them,
// and the conversation, the tools, the approvals and the Approver. A profile brings words and
// helpers, never authority:
//
//	agent.md         required: agentdef front matter (name, description) and the profile's
//	                 instructions (the body)
//	context/*.md     standing context for the profile's work
//	skills/<name>/   skills (SKILL.md), as Nanite reads them
//	variants/*.md    Nanite subtask variants: the profile's internal helpers. Not identities
//	                 of their own; a variant only narrows the parent's tools
//	tools/<name>/    Nanite tool packages (tool.yaml and its entry). Offered only when the
//	                 app's agent config turns Nanite's "tools" group on; every call asks
//	                 the owner first, and an approval holds for one digest of the package
//	steering.json    libs agent/steer v0.1.0 reflexes and presets. Checked, not applied yet:
//	                 Nanite takes steering once, when the agent opens, not per turn
//	evals/           the Workbench's eval suites; ignored here
//	README.md        notes for people; ignored
//
// A profile whose directory has no agent.md and nothing else is an empty slot: declared by
// the module, not written yet. It is not an error and is not offered.
package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/hollis-labs/libs/agent/agentdir"
	"github.com/hollis-labs/libs/agent/steer"
	"github.com/hollis-labs/nanite/agent/authored"
)

// Limits on a profile's files, as agentdir and Nanite bound an agent directory.
const (
	maxFile  = 1 << 20
	maxTotal = 4 << 20
)

// appOwned are agent-directory files a profile may not carry: the app owns the runtime,
// identity, MCP and permissions, and every profile shares them.
var appOwned = map[string]string{
	"host.yaml":        "the runtime is the app's (its agent config and seat)",
	".mcp.json":        "MCP servers are the app's",
	"permissions.yaml": "permissions are the app's (tool levels and the Approver)",
	"providers.yaml":   "the model and gateway are the app's",
	"identity.yaml":    "the identity is the app's: one ADE, one Warden grant",
}

// File is one text file of a profile.
type File struct {
	Name string `json:"name"`
	Text string `json:"-"`
}

// Profile is a module's profile, read and checked.
type Profile struct {
	IgnoredRuntime []string `json:"ignored_runtime,omitempty"`
	// ID is the module's id; it names the profile.
	ID string `json:"id"`
	// Title is the module's title.
	Title string `json:"title"`
	// Name and Description are agent.md's.
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Body is agent.md's text after its front matter.
	Body     string   `json:"-"`
	Context  []File   `json:"context"`
	Skills   []string `json:"skills"`
	Variants []string `json:"variants"`
	// Tools are the tool packages; Steering says steering.json is there (checked, not
	// applied yet).
	Tools    []string `json:"tools"`
	Steering bool     `json:"steering"`

	// files is every file the runner stages, by slash path: agent.md, context/, skills/,
	// variants/, tools/.
	files map[string][]byte
	// exec are the tool packages' entries, which are staged executable (an embedded file
	// has no mode of its own).
	exec        map[string]bool
	steeringRaw []byte
}

// Load reads and checks a module's profile. It returns nil, nil for an empty slot.
func Load(id, title string, fsys fs.FS) (*Profile, error) {
	if fsys == nil {
		return nil, nil
	}
	p := &Profile{ID: id, Title: title, files: map[string][]byte{}, exec: map[string]bool{}, Context: []File{}, Skills: []string{}, Variants: []string{}, Tools: []string{}}
	var steering []byte
	total := 0
	known := 0
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		base := path.Base(name)
		top, _, _ := strings.Cut(name, "/")
		if why, ok := appOwned[top]; ok {
			return fmt.Errorf("%s: a profile may not carry it: %s", top, why)
		}
		if strings.HasPrefix(base, ".") || top == "evals" || name == "README.md" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: a profile may not hold links", name)
		}
		if d.IsDir() {
			switch top {
			case "skills", "context", "variants", "tools":
				return nil
			}
			return fmt.Errorf("%s/: not part of a profile (agent.md, context/, skills/, variants/, tools/, steering.json, evals/)", name)
		}
		switch {
		case name == "agent.md", name == "steering.json":
		case top == "skills", top == "context", top == "variants", top == "tools":
		default:
			return fmt.Errorf("%s: not part of a profile (agent.md, context/, skills/, variants/, tools/, steering.json, evals/)", name)
		}
		known++
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		if len(raw) > maxFile {
			return fmt.Errorf("%s: %d bytes, over %d", name, len(raw), maxFile)
		}
		if total += len(raw); total > maxTotal {
			return fmt.Errorf("the profile's files come to more than %d bytes", maxTotal)
		}
		if name == "steering.json" {
			steering = raw
		} else {
			p.files[name] = raw
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("profile %s: %w", id, err)
	}
	if _, ok := p.files["agent.md"]; !ok {
		if known == 0 {
			return nil, nil // an empty slot
		}
		return nil, fmt.Errorf("profile %s: agent.md is missing", id)
	}
	var errs []error
	if err := p.check(); err != nil {
		errs = append(errs, err)
	}
	if steering != nil {
		if err := checkSteering(steering); err != nil {
			errs = append(errs, fmt.Errorf("steering.json: %w", err))
		}
		p.Steering = true
		p.steeringRaw = steering
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("profile %s: %w", id, err)
	}
	return p, nil
}

// check validates the profile as Nanite will read it: written out beside a stand-in
// host.yaml (the app's seat supplies the real one), then agentdir.Load and Nanite's authored
// validators (variants, tool packages).
func (p *Profile) check() error {
	for name, raw := range p.files {
		pkg, rest, ok := strings.Cut(strings.TrimPrefix(name, "tools/"), "/")
		if !strings.HasPrefix(name, "tools/") || !ok || rest != "tool.yaml" {
			continue
		}
		t, err := authored.ParseTool(pkg, raw)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		p.exec[path.Join("tools", pkg, path.Clean(t.Entry))] = true
	}
	dir, err := os.MkdirTemp("", "surface-profile-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	want := map[string]staged{"host.yaml": {data: []byte(standInHost)}}
	for name, raw := range p.files {
		want[name] = staged{data: raw, exec: p.exec[name]}
	}
	if err = sync(dir, want); err != nil {
		return err
	}
	snap, err := agentdir.Load(dir)
	if err != nil {
		return err
	}
	p.Name, p.Description = snap.Definition.Name, strings.TrimSpace(snap.Definition.Description)
	p.Body = strings.TrimSpace(StripFront(string(p.files["agent.md"])))
	if p.Body == "" {
		return errors.New("agent.md: the body (the profile's instructions) is empty")
	}
	for _, c := range snap.Context {
		p.Context = append(p.Context, File{Name: c.Name, Text: strings.TrimSpace(c.Body)})
	}
	sort.Slice(p.Context, func(i, j int) bool { return p.Context[i].Name < p.Context[j].Name })
	for _, s := range snap.Skills {
		p.Skills = append(p.Skills, s.Name)
	}
	sort.Strings(p.Skills)

	var errs []error
	for name := range p.files {
		if file, ok := strings.CutPrefix(name, "variants/"); ok && (strings.Contains(file, "/") || path.Ext(file) != ".md") {
			errs = append(errs, fmt.Errorf("%s: a variant is variants/<name>.md", name))
		}
	}
	variants, err := authored.LoadVariants(dir)
	if err != nil {
		errs = append(errs, err)
	}
	for name, v := range variants {
		if v.Provider != "" {
			// A provider of its own would be a model and key the app did not choose.
			errs = append(errs, fmt.Errorf("variants/%s.md: provider: a profile's variant runs on the app's provider; leave provider out", name))
			continue
		}
		p.Variants = append(p.Variants, name)
	}
	tools, err := authored.LoadTools(dir)
	if err != nil {
		errs = append(errs, err)
	}
	for name := range tools {
		p.Tools = append(p.Tools, name)
	}
	sort.Strings(p.Variants)
	sort.Strings(p.Tools)
	return errors.Join(errs...)
}

// standInHost is the host.yaml a profile is checked with: an embedded agent, as the app's
// seat is. The app's own host.yaml is what the runner uses.
const standInHost = `kind: embedded
lifetime: application
application_id: surface-profile-check
runtime:
  provider: anthropic
permissions:
  mode: default
`

// splitFront splits a leading --- front matter block from the body, as Nanite does.
func splitFront(raw []byte) (head []byte, body string, err error) {
	text := string(raw)
	first, rest, ok := strings.Cut(text, "\n")
	if !ok || strings.TrimSuffix(first, "\r") != "---" {
		return nil, text, nil
	}
	remaining := rest
	for remaining != "" {
		line, next, _ := strings.Cut(remaining, "\n")
		if strings.TrimSuffix(line, "\r") == "---" {
			return []byte(rest[:len(rest)-len(remaining)]), next, nil
		}
		remaining = next
	}
	return nil, "", errors.New("unterminated frontmatter")
}

// StripFront drops a leading YAML front matter block (--- ... ---).
func StripFront(s string) string {
	_, body, err := splitFront([]byte(strings.TrimLeft(s, "\ufeff\n")))
	if err != nil {
		return s
	}
	return body
}

// checkSteering checks steering.json as the Workbench keeps steering: libs agent/steer
// v0.1.0 presets and reflexes, with the action kinds a Surface host carries out.
func checkSteering(raw []byte) error {
	var s struct {
		Format   int            `json:"workbench_format,omitempty"`
		Presets  []steer.Preset `json:"presets,omitempty"`
		Reflexes []steer.Reflex `json:"reflexes,omitempty"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return err
	}
	v := steer.NewVocabulary()
	for _, kind := range []string{"run_command", "call_tool"} {
		if err := v.Register(kind, nil); err != nil {
			return err
		}
	}
	var errs []error
	reflexes := map[string]bool{}
	for _, r := range s.Reflexes {
		if err := r.Validate(v); err != nil {
			errs = append(errs, err)
		}
		if reflexes[r.Name] {
			errs = append(errs, fmt.Errorf("reflex %s is listed twice", r.Name))
		}
		reflexes[r.Name] = true
	}
	for _, p := range s.Presets {
		if err := p.Validate(); err != nil {
			errs = append(errs, err)
		}
		for _, r := range p.Reflexes {
			if !reflexes[r] {
				errs = append(errs, fmt.Errorf("preset %s names reflex %s, which is not defined", p.Name, r))
			}
		}
	}
	return errors.Join(errs...)
}
