package approvals

import (
	"context"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module"
)

// A reconnect can cancel the stream after Cards.Show commits, before the module
// records the approval/card association. Force exactly that boundary.
type cancelAfterShow struct {
	module.Cards
	cancel context.CancelFunc
}

func (c cancelAfterShow) Show(ctx context.Context, card module.Card) (module.Card, error) {
	shown, err := c.Cards.Show(ctx, card)
	if err == nil {
		c.cancel()
	}
	return shown, err
}

type cardsHost struct {
	module.Host
	cards module.Cards
}

func (h cardsHost) Cards() module.Cards { return h.cards }

func TestReconnectAfterCardCommitKeepsApprovalIdentity(t *testing.T) {
	f := newFake(t)
	h, m := start(t, f)
	_ = m.Stop(t.Context()) // drive the show/replay boundary explicitly
	f.submit("apr_cancel", "approve", "elevated", nil)
	g, err := m.gate()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m.h = cardsHost{Host: h, cards: cancelAfterShow{Cards: h.Cards(), cancel: cancel}}
	if err := m.show(ctx, g, "apr_cancel"); err != nil {
		t.Fatalf("initial show: %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("test did not cancel the stream at publication")
	}
	m.h = h
	if err := m.show(t.Context(), g, "apr_cancel"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	cards := openCards(h)
	if len(cards) != 1 {
		t.Fatalf("%d cards after cancellation/replay, want1: %+v", len(cards), cards)
	}
	row, ok := m.shown(t.Context(), "apr_cancel")
	if !ok || row.card != cards[0].ID {
		t.Fatalf("missing approval identity: %+v %v", row, ok)
	}
}

func TestConcurrentShowsKeepOneApprovalCard(t *testing.T) {
	f := newFake(t)
	h, m := start(t, f)
	_ = m.Stop(t.Context())
	f.submit("apr_concurrent", "approve", "elevated", nil)
	g, err := m.gate()
	if err != nil {
		t.Fatal(err)
	}
	begin := make(chan struct{})
	errs := make(chan error, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); <-begin; errs <- m.show(t.Context(), g, "apr_concurrent") }()
	}
	close(begin)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if cards := openCards(h); len(cards) != 1 {
		t.Fatalf("%d cards for one approval", len(cards))
	}
}
