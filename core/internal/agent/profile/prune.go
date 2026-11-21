package profile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BindingMaxAge is the minimum time an unused snapshot is retained.
const BindingMaxAge = 7 * 24 * time.Hour

// PruneBindings retains the newest keep snapshots, current bindings, and anything
// used within BindingMaxAge. It serializes with staging and recovery touches.
// It deliberately does not promise to retain older interrupted turns.
func PruneBindings(root string, keep int, references func() ([]string, error)) error {
	if keep < 0 || references == nil {
		return errors.New("profile: pruning needs a nonnegative retention count and reference lookup")
	}
	bindingStageMu.Lock()
	defer bindingStageMu.Unlock()
	r, err := os.OpenRoot(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	entries, err := fs.ReadDir(r.FS(), ".")
	if err != nil {
		return err
	}
	type snapshot struct {
		name     string
		lastUsed time.Time
	}
	var snapshots []snapshot
	for _, entry := range entries {
		name := entry.Name()
		// Only our canonical digest directories are candidates. Symlinks, files,
		// and unrelated entries are not staging artifacts we own.
		if !entry.IsDir() || len(name) != 64 || name != strings.ToLower(name) {
			continue
		}
		if _, decodeErr := hex.DecodeString(name); decodeErr != nil {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		snapshots = append(snapshots, snapshot{name, info.ModTime()})
	}
	refs, err := references()
	if err != nil {
		return fmt.Errorf("profile: snapshot references: %w", err)
	}
	protected := make(map[string]bool, len(refs))
	for _, dir := range refs {
		protected[filepath.Clean(dir)] = true
	}
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].lastUsed.Equal(snapshots[j].lastUsed) {
			return snapshots[i].name < snapshots[j].name
		}
		return snapshots[i].lastUsed.After(snapshots[j].lastUsed)
	})
	cutoff := time.Now().Add(-BindingMaxAge)
	for i, snapshot := range snapshots {
		if i < keep || !snapshot.lastUsed.Before(cutoff) || protected[filepath.Join(root, snapshot.name)] {
			continue
		}
		// Detach first: staging this digest again creates a fresh directory.
		// Root confines removal and does not follow nested symlinks.
		tomb := ".prune-" + rand.Text()
		if err = r.Rename(snapshot.name, tomb); err != nil {
			return fmt.Errorf("profile: detach snapshot %s: %w", snapshot.name, err)
		}
		if err = r.RemoveAll(tomb); err != nil {
			return fmt.Errorf("profile: remove snapshot %s: %w", snapshot.name, err)
		}
	}
	return nil
}
