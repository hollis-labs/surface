// Package app is the main of a generated Surface binary: it starts Substrate (config,
// logging, health, the HTTP host, the database), hands the core what Substrate opened, and
// serves. A generated main.go is one call to Main.
//
// Substrate supplies the mechanism; Surface is the app on top of it. The app's own
// settings are not Substrate config: they live in the app's database and change live
// (see the core). Substrate's config holds only what must be known before the database
// opens: the listen address, the log level, the database path.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/hollis-labs/substrate/boot"
	smodule "github.com/hollis-labs/substrate/module"
	"github.com/hollis-labs/substrate/modules/cli"
	"github.com/hollis-labs/substrate/modules/database"
	"github.com/hollis-labs/substrate/modules/httphost"
	"github.com/hollis-labs/substrate/modules/identity"

	"github.com/hollis-labs/surface/core"
	"github.com/hollis-labs/surface/module"
)

// Config is what the generated wiring gives Main.
type Config struct {
	// Name is the app's name; Title is how a person reads it.
	Name  string
	Title string
	// Modules are the Surface modules to start, in order (surface_wiring.go).
	Modules []string
	// SubstrateModules and SubstrateConfigJSON come from substrate_wiring.go.
	SubstrateModules    []string
	SubstrateConfigJSON string
	// Assets is the built page, or nil.
	Assets fs.FS
	// Ready, when set, learns the bound address (tests listen on port 0).
	Ready func(net.Addr)
	// Auth is "" (or "off": one owner, loopback only, no sign-in) or AuthIdentity: people
	// sign in through Substrate's identity module (app/auth.go). surface.yaml's auth sets it.
	Auth string
}

// Main runs the app until it is interrupted, and exits non-zero on failure.
func Main(cfg Config) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := Run(ctx, cfg, os.Getenv, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, cfg.Name+":", err)
		stop()
		os.Exit(1)
	}
}

// envName is the app's prefix for environment variables: "desk" gives "DESK".
func envName(app string) string {
	return strings.ToUpper(strings.NewReplacer("-", "_").Replace(app))
}

// DataDir is where the app keeps its data: <APP>_DATA_DIR, or the app's directory under
// XDG_DATA_HOME (default ~/.local/share).
func DataDir(app string, getenv func(string) string) (string, error) {
	if d := getenv(envName(app) + "_DATA_DIR"); d != "" {
		return filepath.Abs(d)
	}
	base := getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "surface", app), nil
}

// Run starts Substrate and runs the command line.
func Run(ctx context.Context, cfg Config, getenv func(string) string, args []string) error {
	dataDir, err := DataDir(cfg.Name, getenv)
	if err != nil {
		return err
	}
	serving, err := readHTTPS(cfg.Name, getenv)
	if err != nil {
		return err
	}
	started, err := boot.StartWith(ctx, cfg.Name, cfg.SubstrateModules, cfg.SubstrateConfigJSON, boot.Options{
		AfterConfig: func(_ context.Context, app *smodule.App) error {
			if mkErr := os.MkdirAll(dataDir, 0o700); mkErr != nil {
				return fmt.Errorf("prepare the data directory %s: %w", dataDir, mkErr)
			}
			// The core database lives in the data directory unless the operator's override
			// names an absolute path.
			db := child(app.Config, "database")
			sq := child(db, "sqlite")
			if p, _ := sq["path"].(string); !filepath.IsAbs(p) {
				if p == "" {
					p = "surface.db"
				}
				sq["path"] = filepath.Join(dataDir, p)
			}
			// So does the identity signing key, for an app with sign-in.
			if id, ok := app.Config["identity"].(map[string]any); ok {
				if p, _ := id["key_file"].(string); p == "" || !filepath.IsAbs(p) {
					if p == "" {
						p = "identity-signing.key"
					}
					id["key_file"] = filepath.Join(dataDir, p)
				}
			}
			serving.apply(app.Config) // public URL and trusted proxies, for sign-in (https.go)
			if addr := getenv(envName(cfg.Name) + "_ADDR"); addr != "" {
				child(app.Config, "httphost")["addr"] = addr
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	defer func() { _ = started.Stop(context.Background()) }()

	root := cli.Root(started.App)
	if root == nil {
		return errors.New("the cli module must be started")
	}
	serve := &cobra.Command{
		Use: "serve", Short: "Serve the app: its page, its API and its MCP endpoint", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return serveApp(ctx, cfg, started.App, dataDir, serving) },
	}
	root.AddCommand(serve)
	root.AddCommand(clientCommands(cfg, started.App, getenv, dataDir)...)
	root.RunE = func(c *cobra.Command, a []string) error { return serve.RunE(c, a) }
	root.Args = cobra.NoArgs
	root.SetArgs(args)
	return root.ExecuteContext(ctx)
}

func child(m map[string]any, key string) map[string]any {
	c, _ := m[key].(map[string]any)
	if c == nil {
		c = map[string]any{}
		m[key] = c
	}
	return c
}

// listenAddr is the address the app listens on, as the config has it.
func listenAddr(app *smodule.App) string {
	hc, _ := app.Config["httphost"].(map[string]any)
	addr, _ := hc["addr"].(string)
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	return addr
}

func serveApp(ctx context.Context, cfg Config, sapp *smodule.App, dataDir string, serving httpsSettings) error {
	addr := listenAddr(sapp)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("httphost.addr %q: %w", addr, err)
	}
	// Without sign-in the app serves this machine only. With it, Substrate's auth module
	// (enforce) decides what may be bound.
	if ip := net.ParseIP(host); cfg.Auth != AuthIdentity && host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("httphost.addr %q is not loopback: a Surface app without sign-in serves only this machine (set auth: identity in surface.yaml to serve beyond it)", addr)
	}
	st := database.From(sapp)
	if st == nil {
		return errors.New("the database module must be started")
	}
	serving.allowHost()
	var keys module.Keys
	var idsvc *identity.Service
	if cfg.Auth == AuthIdentity {
		if idsvc = identity.From(sapp); idsvc != nil {
			keys = identityKeys{sapp: sapp, svc: idsvc}
		}
	}
	a, err := core.New(ctx, core.Options{
		Name: cfg.Name, Title: cfg.Title, DataDir: dataDir, DB: st.DB(), Log: slog.Default(),
		Modules: cfg.Modules, Assets: cfg.Assets, Keys: keys,
	})
	if err != nil {
		return err
	}
	defer func() { _ = a.Stop(context.Background()) }()
	// Keys made or revoked, by any route or module, reach modules as identity.key.* events.
	if idsvc != nil {
		defer idsvc.WatchKeys(func(e identity.KeyEvent) { a.Publish(module.KeyEventTopic+"."+e.Action, keyEvent(e)) })()
	}
	if err = a.Start(ctx); err != nil {
		return err
	}
	// Substrate's host serves its own routes (/healthz); everything else is the core's. The
	// core serves its API itself rather than as httphost routes because its streams and the
	// agent's wire protocol must reach the page as written (see docs/substrate.md).
	sh, err := httphost.Handler(sapp, httphost.Options{})
	if err != nil {
		return err
	}
	var handler http.Handler
	if cfg.Auth == AuthIdentity {
		if handler, err = authHandler(cfg, sapp, a, sh); err != nil {
			return err
		}
	} else {
		mux := http.NewServeMux()
		mux.Handle("/healthz", sh)
		mux.Handle("/", a.Handler())
		handler = mux
	}
	ln, err := serving.tlsListener(addr)
	if err != nil {
		return err
	}
	scheme := "http"
	if ln != nil {
		scheme = "https"
	}
	slog.Info("surface app serving", "app", cfg.Name, "url", scheme+"://"+addr, "public_url", serving.publicURL, "data", dataDir, "modules", cfg.Modules, "auth", cfg.Auth)
	return httphost.Serve(ctx, sapp, handler, httphost.ServeOptions{Addr: addr, Listener: ln, Ready: cfg.Ready})
}
