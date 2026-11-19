package files

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hollis-labs/surface/module"
)

// The bounds of what one tool call returns.
const (
	listCap          = 500
	readDefaultLines = 2000
)

// target is the place and path a tool call names. Both may be left out: the place then
// comes from the Files window in front, and so does the path when it is left out too.
type target struct {
	Place string  `json:"place"`
	Path  *string `json:"path"`
}

// resolve fills in what the call left out from the focused Files window.
func (t target) resolve(ctx context.Context, h module.Host) (place, rel string, err error) {
	if t.Path != nil {
		rel = *t.Path
	}
	if t.Place != "" {
		return t.Place, rel, nil
	}
	place, frontRel, ok := front(ctx, h)
	if !ok {
		return "", "", errors.New("say which place: no Files window is in front to take it from (files_places lists the places)")
	}
	if t.Path == nil {
		rel = frontRel
	}
	return place, rel, nil
}

// label names a call's target for the permission question.
func (t target) label() string {
	rel := ""
	if t.Path != nil {
		rel = *t.Path
	}
	if t.Place == "" {
		return rel
	}
	return t.Place + ":" + rel
}

const (
	placeProp = `"place":{"type":"string","description":"A place id from files_places; \"remote:<id>\" is a folder on another host, reached over ssh. Left out: the place of the Files window in front."}`
	pathHelp  = `Relative to the place, with forward slashes (\"notes/todo.md\"); \"\" is the place's root.`
)

func (m *mod) tools() []module.Tool {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	return []module.Tool{
		{Name: "files_places", Level: module.LevelRead,
			Description: "List the owner's places: the named root folders the files tools can reach. Most are on this machine; those with remote: true (id \"remote:<id>\", with a host) are folders on another host, reached over ssh, and every files tool works on them the same way. Every other files tool takes a place id from here and a path relative to that place. Call it first when you do not know the place.",
			Input:       raw(`{"type":"object","properties":{}}`),
			Run: func(ctx context.Context, _ module.Host, _ json.RawMessage) (any, error) {
				return m.places(ctx)
			}},
		{Name: "files_list", Level: module.LevelRead,
			Description: "List a folder: names, sizes and modification times, folders first. With no place and no path it lists the folder in the Files window in front, which is what the owner means by \"this folder\". Dotfiles are left out unless the owner's show-hidden setting is on or hidden is true. Entries marked locked look like credentials and cannot be read.",
			Input:       raw(`{"type":"object","properties":{` + placeProp + `,"path":{"type":"string","description":"The folder. ` + pathHelp + ` Left out with place: the folder in front."},"hidden":{"type":"boolean","description":"Include dotfiles."}}}`),
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct {
					target
					Hidden bool `json:"hidden"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				place, rel, err := a.resolve(ctx, h)
				if err != nil {
					return nil, err
				}
				rel, entries, err := m.listing(ctx, place, rel, a.Hidden || h.Config().Bool("show_hidden"))
				if err != nil {
					return nil, err
				}
				out := map[string]any{"place": place, "path": rel, "count": len(entries), "entries": entries}
				if len(entries) > listCap {
					out["entries"], out["truncated"] = entries[:listCap], true
				}
				return out, nil
			}},
		{Name: "files_read", Level: module.LevelRead,
			Description: fmt.Sprintf("Read a text file. Returns the text with the line range it covers, the file's size, and truncated: true when the file goes on. One call returns at most %d lines and the owner's preview limit in bytes (256 KB by default); read on with offset set to end_line + 1. Binary files and files that look like credentials are refused.", readDefaultLines),
			Input:       raw(`{"type":"object","required":["path"],"properties":{` + placeProp + `,"path":{"type":"string","description":"The file. ` + pathHelp + `"},"offset":{"type":"integer","minimum":1,"description":"The first line to return, counted from 1."},"limit":{"type":"integer","minimum":1,"description":"How many lines to return."}}}`),
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct {
					target
					Offset int `json:"offset"`
					Limit  int `json:"limit"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if a.Path == nil || *a.Path == "" {
					return nil, errors.New("say which file to read (path)")
				}
				place, rel, err := a.resolve(ctx, h)
				if err != nil {
					return nil, err
				}
				if a.Limit <= 0 || a.Limit > readDefaultLines {
					a.Limit = readDefaultLines
				}
				rel, t, err := m.read(ctx, place, rel, a.Offset, a.Limit, previewLimit(h))
				if err != nil {
					return nil, err
				}
				return struct {
					Place string `json:"place"`
					Path  string `json:"path"`
					Text
				}{place, rel, t}, nil
			}},
		{Name: "files_grep", Level: module.LevelRead,
			Description: fmt.Sprintf("Search the text of the files under a folder (or in one file) and return the matching lines with their file and line number. query is plain text matched without regard to case, or a regular expression (RE2 syntax, case sensitive unless it starts with (?i)) when regexp is true. Use glob to keep to some files (\"*.md\"). The search is bounded: %d folders deep, %d files, %d matches unless limit says otherwise; it skips binary files, files over 1 MB, symbolic links, credentials, and folders like .git and node_modules. truncated: true means a bound was reached, so narrow the folder or the query. In a remote place the host's own find and grep do the search, and a regular expression runs as POSIX extended (no \\d; use [0-9]).", grepMaxDepth, grepMaxFiles, grepMaxMatches),
			Input:       raw(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"},"regexp":{"type":"boolean","description":"Treat query as a regular expression."},` + placeProp + `,"path":{"type":"string","description":"The folder to search under, or one file. ` + pathHelp + ` Left out with place: the folder in front."},"glob":{"type":"string","description":"A file name pattern, like *.md."},"max_depth":{"type":"integer","minimum":1,"description":"How many folders deep to go."},"limit":{"type":"integer","minimum":1,"maximum":500,"description":"The most matches to return."}}}`),
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct {
					target
					Query    string `json:"query"`
					Regexp   bool   `json:"regexp"`
					Glob     string `json:"glob"`
					MaxDepth int    `json:"max_depth"`
					Limit    int    `json:"limit"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				place, rel, err := a.resolve(ctx, h)
				if err != nil {
					return nil, err
				}
				q := grepQuery{Query: a.Query, Regexp: a.Regexp, Glob: a.Glob, MaxDepth: a.MaxDepth, MaxMatches: a.Limit}
				if isRemote(place) {
					r, rerr := m.remote(place)
					if rerr != nil {
						return nil, rerr
					}
					return r.grep(ctx, rel, q)
				}
				l, err := m.at(ctx, place, rel, true)
				if err != nil {
					return nil, err
				}
				return grep(ctx, l, q)
			}},
		{Name: "files_write", Level: module.LevelWrite,
			Description: "Create a text file, or replace the whole content of one. text is the complete new content: to change part of a file, read it first and write it back whole. Missing parent folders are created. Set create_only to fail when the file already exists. It will not replace a folder, a binary file or a file that looks like a credential.",
			Input:       raw(`{"type":"object","required":["path","text"],"properties":{` + placeProp + `,"path":{"type":"string","description":"The file. ` + pathHelp + `"},"text":{"type":"string","description":"The file's whole new content."},"create_only":{"type":"boolean","description":"Fail when the file already exists."}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct {
					target
					Text string `json:"text"`
				}
				_ = json.Unmarshal(in, &a)
				return fmt.Sprintf("Write %s (%d bytes)", a.label(), len(a.Text))
			},
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct {
					target
					Text       *string `json:"text"`
					CreateOnly bool    `json:"create_only"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if a.Path == nil || *a.Path == "" || a.Text == nil {
					return nil, errors.New("files_write needs the file's path and its text")
				}
				place, rel, err := a.resolve(ctx, h)
				if err != nil {
					return nil, err
				}
				return m.write(ctx, h, place, rel, *a.Text, a.CreateOnly)
			}},
		{Name: "files_mkdir", Level: module.LevelWrite,
			Description: "Create a folder, with any missing parent folders. Fails when something of that name is already there.",
			Input:       raw(`{"type":"object","required":["path"],"properties":{` + placeProp + `,"path":{"type":"string","description":"The new folder. ` + pathHelp + `"}}}`),
			Describe: func(in json.RawMessage) string {
				var a target
				_ = json.Unmarshal(in, &a)
				return "Create folder " + a.label()
			},
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a target
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if a.Path == nil || *a.Path == "" {
					return nil, errors.New("say what to call the new folder (path)")
				}
				place, rel, err := a.resolve(ctx, h)
				if err != nil {
					return nil, err
				}
				return m.mkdir(ctx, h, place, rel)
			}},
		{Name: "files_move", Level: module.LevelConfirm,
			Description: "Rename or move a file or folder inside one place. to is the full new path, including the name (to move a.md into docs, to is \"docs/a.md\"); its folder must exist. It never replaces anything: a taken destination fails, so pick another name. It cannot move between places, so not between this machine and a remote host.",
			Input:       raw(`{"type":"object","required":["from","to"],"properties":{` + placeProp + `,"from":{"type":"string","description":"What to move. ` + pathHelp + `"},"to":{"type":"string","description":"Its new path, including the name."}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ Place, From, To string }
				_ = json.Unmarshal(in, &a)
				return fmt.Sprintf("Move %s to %s", target{Place: a.Place, Path: &a.From}.label(), a.To)
			},
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct{ Place, From, To string }
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if a.From == "" || a.To == "" {
					return nil, errors.New("files_move needs from and to")
				}
				place, _, err := target{Place: a.Place, Path: &a.From}.resolve(ctx, h)
				if err != nil {
					return nil, err
				}
				return m.move(ctx, h, place, a.From, a.To)
			}},
		{Name: "files_delete", Level: module.LevelConfirm,
			Description: "Delete one file, or one empty folder. It is permanent: nothing goes to a trash. There is no recursive delete; a folder with anything in it is refused, so its contents must be deleted first, one call each.",
			Input:       raw(`{"type":"object","required":["path"],"properties":{` + placeProp + `,"path":{"type":"string","description":"The file or empty folder. ` + pathHelp + `"}}}`),
			Describe: func(in json.RawMessage) string {
				var a target
				_ = json.Unmarshal(in, &a)
				return "Delete " + a.label() + " for good"
			},
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a target
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if a.Path == nil || *a.Path == "" {
					return nil, errors.New("say what to delete (path)")
				}
				place, rel, err := a.resolve(ctx, h)
				if err != nil {
					return nil, err
				}
				return m.delete(ctx, h, place, rel)
			}},
		{Name: "files_open", Level: module.LevelAct,
			Description: "Show a folder to the owner in the Files window, or a file (its folder opens with the file selected and previewed). Use it to point at something; it changes nothing on disk. new_window opens a second Files window instead of taking the main one there.",
			Input:       raw(`{"type":"object","required":["place"],"properties":{"place":{"type":"string","description":"A place id from files_places."},"path":{"type":"string","description":"The folder or file. ` + pathHelp + `"},"new_window":{"type":"boolean"}}}`),
			Describe: func(in json.RawMessage) string {
				var a target
				_ = json.Unmarshal(in, &a)
				return "Open " + a.label() + " in Files"
			},
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct {
					Place     string `json:"place"`
					Path      string `json:"path"`
					NewWindow bool   `json:"new_window"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if a.Place == "" {
					return nil, errors.New("say which place to open (files_places lists them)")
				}
				w, err := m.open(ctx, h, a.Place, a.Path, a.NewWindow)
				if err != nil {
					return nil, err
				}
				return map[string]any{"window": w.ID, "place": w.Args["place"], "path": w.Args["path"], "selected": w.Args["select"]}, nil
			}},
	}
}
