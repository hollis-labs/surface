package agent

import (
	"fmt"
	"path/filepath"

	"github.com/hollis-labs/surface/core/internal/agent/profile"
)

const bindingSnapshotsToKeep = 10

// PruneBindingSnapshots refreshes current bindings before collecting old unused
// snapshots. A missing or invalid bound folder skips pruning altogether.
func (s *Service) PruneBindingSnapshots() {
	if err := s.pruneBindingSnapshots(); err != nil {
		s.log.Warn("bound agent snapshots not pruned", "err", err)
	}
}

func (s *Service) pruneBindingSnapshots() error {
	root := filepath.Join(s.k.DataDir(), "agents", "bindings")
	var current []string
	if s.bindings != nil {
		for id, dir := range s.bindings() {
			p, err := profile.LoadBinding(id, s.modules[id], dir)
			if err != nil {
				return fmt.Errorf("binding %s: %w", id, err)
			}
			staged, err := p.StageBinding(root)
			if err != nil {
				return err
			}
			current = append(current, staged)
		}
	}
	return profile.PruneBindings(root, bindingSnapshotsToKeep, func() ([]string, error) { return current, nil })
}
