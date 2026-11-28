// Command surface generates and builds Surface apps.
//
// A Surface app is a directory with a surface.yaml: the app's name, the modules it is made
// of and where it listens. From it `surface generate` writes the Substrate manifest and
// wiring, the Surface wiring (which modules are compiled in, and in what order), a main
// and the page's module list; `surface build` then builds the page and the binary.
//
//	surface generate -f examples/demo/surface.yaml
//	surface build    -f examples/demo/surface.yaml
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// manifest is surface.yaml.
type manifest struct {
	// App is the binary's name; Title is how a person reads it.
	App   string `yaml:"app"`
	Title string `yaml:"title"`
	// Addr is the default listen address. It must be loopback.
	Addr string `yaml:"addr"`
	// Modules are the modules compiled in, in start order. An entry is a module id from
	// this repository's modules/ directory, or a mapping for one that lives elsewhere.
	Modules []moduleRef `yaml:"modules"`
	// Log is Substrate's logging block.
	Log map[string]any `yaml:"log"`
	// Auth is "off" (the default: one owner, loopback only) or "identity": people sign in
	// through Substrate's identity module (users, sessions, roles, API keys), and the app may
	// serve beyond loopback. The owner is made with a setup link ("<app> identity setup-link").
	Auth string `yaml:"auth"`
}

// moduleRef names a module: its id, its Go import path and its web entry.
type moduleRef struct {
	ID     string `yaml:"id"`
	Import string `yaml:"import"`
	// Web is the module's web entry, relative to the repository root. Empty means the
	// module has no views of its own. A Surface module used by an app in another
	// repository gets a "@surface/modules/<id>" specifier here instead: surfacePage()
	// (@hollis-labs/surface-page/vite) resolves it from the Surface module on disk.
	Web string `yaml:"web"`
}

// surfaceSpecifier is the page import of a Surface module's views in an app outside the
// Surface repository. It is not a path, so the generated file is the same on every machine.
const surfaceSpecifier = "@surface/modules/"

func (m *moduleRef) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		m.ID = n.Value
		return nil
	}
	type plain moduleRef
	return n.Decode((*plain)(m))
}

var idRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

const surfaceModule = "github.com/hollis-labs/surface"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	file := fs.String("f", "surface.yaml", "the app's surface.yaml")
	skipWeb := fs.Bool("skip-web", false, "build: do not rebuild the page")
	_ = fs.Parse(os.Args[2:])
	var err error
	switch cmd {
	case "generate":
		_, err = generate(*file)
	case "build":
		err = build(*file, *skipWeb)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "surface:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: surface generate|build -f <app>/surface.yaml [-skip-web]")
	os.Exit(2)
}

// app is a loaded manifest with its paths resolved.
type app struct {
	manifest
	dir  string // the app's directory
	root string // the repository root (where go.mod is)
	pkg  string // the app's Go import path
	rel  string // the app's directory, relative to the root
}

func load(file string) (*app, error) {
	raw, err := os.ReadFile(file) //nolint:gosec // the manifest path is the developer's own argument
	if err != nil {
		return nil, err
	}
	var m manifest
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err = dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if !idRe.MatchString(m.App) {
		return nil, fmt.Errorf("%s: app must be lower case letters, digits and dashes", file)
	}
	if m.Title == "" {
		m.Title = m.App
	}
	if m.Addr == "" {
		m.Addr = "127.0.0.1:8190"
	}
	switch m.Auth {
	case "", "off":
		m.Auth = ""
	case "identity":
	default:
		return nil, fmt.Errorf("%s: auth must be off or identity", file)
	}
	if len(m.Modules) == 0 {
		return nil, fmt.Errorf("%s: an app needs at least one module", file)
	}
	dir, err := filepath.Abs(filepath.Dir(file))
	if err != nil {
		return nil, err
	}
	root := dir
	for {
		if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr == nil { //nolint:gosec // walks up from the developer's manifest to the repository root
			break
		}
		up := filepath.Dir(root)
		if up == root {
			return nil, errors.New("no go.mod above " + dir)
		}
		root = up
	}
	modRaw, err := os.ReadFile(filepath.Join(root, "go.mod")) //nolint:gosec // the repository's own go.mod
	if err != nil {
		return nil, err
	}
	modPath := ""
	for _, line := range strings.Split(string(modRaw), "\n") {
		if strings.HasPrefix(line, "module ") {
			modPath = strings.TrimSpace(strings.TrimPrefix(line, "module "))
			break
		}
	}
	rel, _ := filepath.Rel(root, dir)
	a := &app{manifest: m, dir: dir, root: root, pkg: modPath + "/" + filepath.ToSlash(rel), rel: filepath.ToSlash(rel)}
	seen := map[string]bool{}
	for i := range a.Modules {
		r := &a.Modules[i]
		if !idRe.MatchString(r.ID) {
			return nil, fmt.Errorf("%s: module id %q is not valid", file, r.ID)
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("%s: module %q is listed twice", file, r.ID)
		}
		seen[r.ID] = true
		if r.Import == "" {
			// A module of this repository (or, in another repository, of Surface itself).
			local := filepath.Join(root, "modules", r.ID)
			if _, err := os.Stat(local); err == nil { //nolint:gosec // a module directory of the developer's repository; the id is checked above
				r.Import = modPath + "/modules/" + r.ID
				if _, err := os.Stat(filepath.Join(local, "web", "index.tsx")); err == nil { //nolint:gosec // same module directory
					r.Web = "modules/" + r.ID + "/web/index.tsx"
				}
			} else {
				r.Import = surfaceModule + "/modules/" + r.ID
				if modPath != surfaceModule {
					dir, err := surfaceDir(root)
					if err != nil {
						return nil, err
					}
					if _, err := os.Stat(filepath.Join(dir, "modules", r.ID, "web", "index.tsx")); err == nil { //nolint:gosec // the Surface module in the Go module cache; the id is checked above
						r.Web = surfaceSpecifier + r.ID
					}
				}
			}
		}
	}
	return a, nil
}

// surfaceDir is where the Surface module the app requires is on disk (the Go module cache, or a
// replace target), as `go list -m` reports it from the app's repository.
var surfaceDirs = map[string]string{}

func surfaceDir(root string) (string, error) {
	if d, ok := surfaceDirs[root]; ok {
		return d, nil
	}
	list := func() (string, error) {
		c := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", surfaceModule) //nolint:gosec // fixed arguments
		c.Dir, c.Stderr = root, os.Stderr
		out, err := c.Output()
		if err != nil {
			return "", fmt.Errorf("find %s from %s (is it in go.mod?): %w", surfaceModule, root, err)
		}
		return strings.TrimSpace(string(out)), nil
	}
	d, err := list()
	if err != nil {
		return "", err
	}
	if d == "" { // required but not downloaded yet (a fresh CI checkout)
		if err = run(root, "go", "mod", "download", surfaceModule); err != nil {
			return "", fmt.Errorf("go mod download %s: %w", surfaceModule, err)
		}
		if d, err = list(); err != nil {
			return "", err
		}
		if d == "" {
			return "", fmt.Errorf("go list -m %s: no directory after go mod download", surfaceModule)
		}
	}
	surfaceDirs[root] = d
	return d, nil
}

func write(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // source directories in the developer's repository
		return err
	}
	if old, err := os.ReadFile(path); err == nil && string(old) == body { //nolint:gosec // a generated file inside the app's directory
		return nil
	}
	return os.WriteFile(path, []byte(body), 0o644) //nolint:gosec // generated source files are world-readable like the rest of the repository
}

func run(dir string, name string, args ...string) error {
	c := exec.Command(name, args...) //nolint:gosec // only go, npm and npx, with fixed arguments
	c.Dir, c.Stdout, c.Stderr = dir, os.Stdout, os.Stderr
	return c.Run()
}

// generate writes everything that follows from surface.yaml.
func generate(file string) (*app, error) {
	a, err := load(file)
	if err != nil {
		return nil, err
	}
	logging := a.Log
	if logging == nil {
		logging = map[string]any{"level": "info", "format": "text"}
	}
	subModules := []string{"database", "cli"}
	subConfig := map[string]any{
		"logging":  logging,
		"httphost": map[string]any{"addr": a.Addr},
		// Relative: the binary joins it onto the app's data directory at boot.
		"database": map[string]any{"sqlite": map[string]any{"path": "surface.db"}},
	}
	if a.Auth == "identity" {
		subModules = append(subModules, "auth", "identity")
		subConfig["auth"] = map[string]any{"mode": "enforce"}
		// key_file is relative, joined onto the data directory at boot like the database.
		subConfig["identity"] = map[string]any{"key_file": "identity-signing.key", "setup": "offline"}
	}
	sub := map[string]any{
		"app": a.App, "variant": "app-host", "profiles": []string{"minimal"},
		"modules": map[string]any{"add": subModules},
		"config":  subConfig,
	}
	y, err := yaml.Marshal(sub)
	if err != nil {
		return nil, err
	}
	if err := write(filepath.Join(a.dir, "substrate.yaml"),
		"# Generated by `surface generate` from surface.yaml; DO NOT EDIT.\n# Substrate supplies the shell (config, logging, health, HTTP host, database, CLI skeleton).\n# The app's own settings are not here: they live in its database and change live.\n"+string(y)); err != nil {
		return nil, err
	}
	if err := run(a.root, "go", "run", "github.com/hollis-labs/substrate/cmd/substrate", "generate", "-f", filepath.Join(a.dir, "substrate.yaml")); err != nil {
		return nil, fmt.Errorf("substrate generate: %w", err)
	}

	var imports, ids strings.Builder
	for _, r := range a.Modules {
		fmt.Fprintf(&imports, "\t_ %q\n", r.Import)
		fmt.Fprintf(&ids, "%q, ", r.ID)
	}
	wiring := fmt.Sprintf(`// Code generated by surface from surface.yaml; DO NOT EDIT.

package main

import (
%s)

const (
	surfaceApp   = %q
	surfaceTitle = %q
	surfaceAuth  = %q
)

// surfaceModules is the start order.
var surfaceModules = []string{%s}
`, imports.String(), a.App, a.Title, a.Auth, strings.TrimSuffix(ids.String(), ", "))
	if err := write(filepath.Join(a.dir, "surface_wiring.go"), wiring); err != nil {
		return nil, err
	}

	mainGo := fmt.Sprintf(`// Code generated by surface from surface.yaml; DO NOT EDIT.

// Command %s is a Surface app: the Surface core plus the modules surface.yaml lists.
package main

import (
	"github.com/hollis-labs/surface/app"

	"%s/webui"
)

func main() {
	assets, _ := webui.Assets()
	app.Main(app.Config{
		Name: surfaceApp, Title: surfaceTitle, Modules: surfaceModules, Auth: surfaceAuth,
		SubstrateModules: substrateModules, SubstrateConfigJSON: substrateConfigJSON,
		Assets: assets,
	})
}
`, a.App, a.pkg)
	if err := write(filepath.Join(a.dir, "main.go"), mainGo); err != nil {
		return nil, err
	}
	embed := `// Code generated by surface from surface.yaml; DO NOT EDIT.

// Package webui embeds the app's built page (dist/).
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// Assets returns the built page rooted at dist/, or nil when the page was not built: an
// unbuilt tree holds only dist/.gitkeep, which still compiles.
func Assets() (fs.FS, error) {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, nil
	}
	return sub, nil
}
`
	if err := write(filepath.Join(a.dir, "webui", "embed.go"), embed); err != nil {
		return nil, err
	}
	keep := filepath.Join(a.dir, "webui", "dist", ".gitkeep")
	if _, err := os.Stat(keep); err != nil { //nolint:gosec // a generated file inside the app's directory
		if err := write(keep, ""); err != nil {
			return nil, err
		}
	}

	// The page's side: which modules' views are compiled into this app's page.
	var web strings.Builder
	web.WriteString("// Code generated by surface from surface.yaml; DO NOT EDIT.\nimport type { ModuleWeb } from \"@hollis-labs/surface-page/sdk\"\n")
	var entries []string
	genDir := filepath.Join(a.root, "web", "src", "generated")
	for i, r := range a.Modules {
		if r.Web == "" {
			continue
		}
		spec := r.Web
		if !strings.HasPrefix(spec, surfaceSpecifier) {
			rel, err := filepath.Rel(genDir, filepath.Join(a.root, strings.TrimSuffix(r.Web, filepath.Ext(r.Web))))
			if err != nil {
				return nil, err
			}
			spec = filepath.ToSlash(rel)
		}
		fmt.Fprintf(&web, "import m%d from %q\n", i, spec)
		entries = append(entries, fmt.Sprintf("  %q: m%d,", r.ID, i))
	}
	fmt.Fprintf(&web, "\nexport const app = { name: %q, title: %q }\n\nexport const modules: Record<string, ModuleWeb> = {\n%s\n}\n", a.App, a.Title, strings.Join(entries, "\n"))
	if err := write(filepath.Join(genDir, a.App+".ts"), web.String()); err != nil {
		return nil, err
	}
	fmt.Printf("generated %s: %d modules\n", a.App, len(a.Modules))
	return a, nil
}

// build generates, builds the page into the app's webui/dist, then builds the binary.
func build(file string, skipWeb bool) error {
	a, err := generate(file)
	if err != nil {
		return err
	}
	if !skipWeb {
		// The page's packages are installed at the repository root, so that a module's web
		// code (modules/<id>/web) resolves them as the core's (web/) does.
		if _, err := os.Stat(filepath.Join(a.root, "node_modules")); err != nil {
			if err := run(a.root, "npm", "ci"); err != nil {
				return fmt.Errorf("npm ci: %w", err)
			}
		}
		c := exec.Command("npx", "vite", "build", "--config", "web/vite.config.ts")
		c.Dir, c.Stdout, c.Stderr = a.root, os.Stdout, os.Stderr
		c.Env = append(os.Environ(), "SURFACE_APP="+a.App, "SURFACE_OUT="+filepath.Join(a.dir, "webui", "dist"))
		if err := c.Run(); err != nil {
			return fmt.Errorf("build the page: %w", err)
		}
	}
	out := filepath.Join(a.root, "bin", a.App)
	if err := run(a.root, "go", "build", "-o", out, "./"+a.rel); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	fmt.Println("built", out)
	return nil
}
