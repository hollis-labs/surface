package agent

import (
	"testing"

	"github.com/hollis-labs/surface/core/internal/agent/hostrunner"
)

// The host runner takes the context block whole (no Nanite slot to fit), and compacts at
// Basalisk's 110,000 tokens unless the config says otherwise.
func TestTheHostRunnerHasNoContextCapAndCompactsAtBasalisks(t *testing.T) {
	s := &Service{wire: &wire{host: &hostrunner.Runner{}}, cfg: FileConfig{System: "Be brief."}}
	if got := s.contextLimit(); got != 0 {
		t.Errorf("contextLimit with the host runner: %d, want 0", got)
	}
	s.wire = &wire{}
	if got := s.contextLimit(); got != 0 {
		t.Errorf("contextLimit with the scripted runner: %d, want 0", got)
	}
	for in, want := range map[int]int{0: 110_000, 50_000: 50_000, -1: -1} {
		if got := compactAt(in); got != want {
			t.Errorf("compactAt(%d) = %d, want %d", in, got, want)
		}
	}
}
