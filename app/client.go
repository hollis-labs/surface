package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	smodule "github.com/hollis-labs/substrate/module"
)

// clientCommands are the commands that drive a running app over its API: the CLI path for
// a person, a script or an outside agent.
func clientCommands(cfg Config, sapp *smodule.App, getenv func(string) string) []*cobra.Command {
	base := func() string {
		addr := listenAddr(sapp)
		if a := getenv(envName(cfg.Name) + "_ADDR"); a != "" {
			addr = a
		}
		return "http://" + addr
	}
	// callFor is call with a client timeout (0: none).
	var callFor func(c *cobra.Command, timeout time.Duration, method, path string, body any) error
	call := func(c *cobra.Command, method, path string, body any) error {
		return callFor(c, 60*time.Second, method, path, body)
	}
	callFor = func(c *cobra.Command, timeout time.Duration, method, path string, body any) error {
		var rd io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				return err
			}
			rd = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(c.Context(), method, base()+path, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := (&http.Client{Timeout: timeout}).Do(req)
		if err != nil {
			return fmt.Errorf("%s is not running at %s (start it with `%s serve`): %w", cfg.Name, base(), cfg.Name, err)
		}
		defer func() { _ = res.Body.Close() }()
		out, _ := io.ReadAll(res.Body)
		if res.StatusCode >= 400 {
			var e struct{ Error string }
			if json.Unmarshal(out, &e) == nil && e.Error != "" {
				return errors.New(e.Error)
			}
			return fmt.Errorf("%s: %s", res.Status, strings.TrimSpace(string(out)))
		}
		var pretty bytes.Buffer
		if json.Indent(&pretty, out, "", "  ") == nil {
			out = pretty.Bytes()
		}
		_, err = fmt.Fprintln(c.OutOrStdout(), strings.TrimSpace(string(out)))
		return err
	}
	jsonArg := func(args []string) (json.RawMessage, error) {
		if len(args) < 2 {
			return json.RawMessage(`{}`), nil
		}
		if !json.Valid([]byte(args[1])) {
			return nil, errors.New("the argument is not valid JSON")
		}
		return json.RawMessage(args[1]), nil
	}

	desk := &cobra.Command{Use: "desk", Short: "Show the windows of the running app", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return call(c, "GET", "/api/desk", nil) }}
	open := &cobra.Command{Use: "open <view> [args-json]", Short: "Open a view in the running app", Args: cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			a, err := jsonArg(args)
			if err != nil {
				return err
			}
			return call(c, "POST", "/api/desk/actions", map[string]any{"type": "open", "view": args[0], "args": a})
		}}
	run := &cobra.Command{Use: "run <command-id> [args-json]", Short: "Run a palette command in the running app", Args: cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			a, err := jsonArg(args)
			if err != nil {
				return err
			}
			return call(c, "POST", "/api/commands/"+args[0], map[string]any{"args": a})
		}}
	tools := &cobra.Command{Use: "tools", Short: "List the tools the running app offers agents", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return call(c, "GET", "/api/tools", nil) }}
	tool := &cobra.Command{Use: "call <tool> [input-json]", Short: "Call one of the running app's tools", Args: cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			a, err := jsonArg(args)
			if err != nil {
				return err
			}
			// A tool may wait on purpose (an agent's turn, a long job): no client timeout unless
			// one is asked for. Ctrl-C still cancels.
			timeout, _ := c.Flags().GetDuration("timeout")
			return callFor(c, timeout, "POST", "/api/tools/"+args[0], a)
		}}
	tool.Flags().Duration("timeout", 0, "give up after this long (e.g. 90s, 10m); 0 waits as long as the tool takes")
	settings := &cobra.Command{Use: "settings [key] [value-json]", Short: "Show the live settings, or change one", Args: cobra.MaximumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 2 {
				return call(c, "GET", "/api/settings", nil)
			}
			v, err := jsonArg(args)
			if err != nil {
				// A bare word is a string.
				raw, _ := json.Marshal(args[1])
				v = raw
			}
			return call(c, "PUT", "/api/settings/"+args[0], map[string]any{"value": v})
		}}
	layouts := &cobra.Command{Use: "layouts [name]", Short: "List the saved layouts, or arrange the desk from one", Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return call(c, "GET", "/api/layouts", nil)
			}
			return call(c, "POST", "/api/layouts/"+args[0]+"/load", nil)
		}}
	return []*cobra.Command{desk, open, run, tools, tool, settings, layouts}
}
