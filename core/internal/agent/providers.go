package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hollis-labs/nanite/agent/authored"
)

// The app's providers file: Nanite's providers.yaml kept outside the agent directory (see
// FileConfig.ProvidersFile), staged into the directory the runner opens before each turn.

// ProvidersPath is where the app's providers file is read from.
func (c FileConfig) ProvidersPath(dataDir string) string {
	if c.ProvidersFile != "" {
		return c.ProvidersFile
	}
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "agent", "providers.yaml")
}

// appProviders reads and checks the app's providers file. A missing file is (nil, false, nil).
// chat reports whether it selects a provider for use.chat.
func appProviders(path string) (raw []byte, chat bool, err error) {
	if path == "" {
		return nil, false, nil
	}
	raw, err = os.ReadFile(path) //nolint:gosec // the app's own providers file, by design
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	p, err := authored.ParseProviders(raw)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	_, chat = p.Use[authored.UseChat]
	return raw, chat, nil
}

// stagedAppFiles is what the app lays over the staged agent directory each turn: its
// providers file as providers.yaml. A file that does not check out is left out (the turn
// runs on the gateway, or says no provider is set), with the reason in the log.
func (s *Service) stagedAppFiles() map[string][]byte {
	raw, _, err := appProviders(s.cfg.ProvidersPath(s.k.DataDir()))
	if err != nil {
		s.log.Error("the app's providers file does not check out; it is left out", "err", err)
		return nil
	}
	if raw == nil {
		return nil
	}
	return map[string][]byte{"providers.yaml": raw}
}
