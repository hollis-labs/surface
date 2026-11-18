package moduletest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/hollis-labs/surface/module"
)

// Cards is a module.Cards kept in memory. A test answers a card with Respond and reads
// what the module showed from Shown.
type Cards struct {
	mu    sync.Mutex
	Shown []module.Card
}

func (c *Cards) find(id string) int {
	for i, x := range c.Shown {
		if x.ID == id {
			return i
		}
	}
	return -1
}

func (c *Cards) Show(_ context.Context, card module.Card) (module.Card, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if card.Kind == "" {
		return card, errors.New("a card needs a kind")
	}
	card.ID, card.Status, card.Revision = fmt.Sprintf("cd_%d", len(c.Shown)+1), "", 1
	c.Shown = append(c.Shown, card)
	return card, nil
}

func (c *Cards) Update(_ context.Context, id, title string, payload json.RawMessage) (module.Card, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.find(id)
	if i < 0 {
		return module.Card{}, errors.New("no such card")
	}
	if title != "" {
		c.Shown[i].Title = title
	}
	if len(payload) > 0 {
		c.Shown[i].Payload = payload
	}
	c.Shown[i].Revision++
	return c.Shown[i], nil
}

func (c *Cards) End(_ context.Context, id, status string) (module.Card, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.find(id)
	if i < 0 {
		return module.Card{}, errors.New("no such card")
	}
	if c.Shown[i].Status == "" {
		c.Shown[i].Status = status
	}
	return c.Shown[i], nil
}

func (c *Cards) Respond(_ context.Context, id string, o module.CardOutcome) (module.Card, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.find(id)
	if i < 0 {
		return module.Card{}, errors.New("no such card")
	}
	if c.Shown[i].Status != "" {
		return c.Shown[i], errors.New("the card is no longer open")
	}
	c.Shown[i].Status, c.Shown[i].Outcome = o.Status, &o
	return c.Shown[i], nil
}

// Await returns the card as it is: a test answers before the module waits, or reads the
// open card back.
func (c *Cards) Await(ctx context.Context, id string) (module.Card, error) {
	card, ok, _ := c.Get(ctx, id)
	if !ok {
		return card, errors.New("no such card")
	}
	return card, nil
}

func (c *Cards) Get(_ context.Context, id string) (module.Card, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i := c.find(id); i >= 0 {
		return c.Shown[i], true, nil
	}
	return module.Card{}, false, nil
}

func (c *Cards) List(_ context.Context, all bool) ([]module.Card, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []module.Card{}
	for _, x := range c.Shown {
		if all || x.Status == "" {
			out = append(out, x)
		}
	}
	return out, nil
}

var _ module.Cards = (*Cards)(nil)
