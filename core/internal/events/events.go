// Package events is the app's event bus: in-process publish and subscribe by topic prefix.
package events

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/hollis-labs/surface/module"
)

// Bus delivers events to subscribers. The zero value is not usable; call New.
type Bus struct {
	mu   sync.RWMutex
	next int
	subs map[int]sub
}

type sub struct {
	prefix string
	fn     func(module.Event)
}

// New returns an empty bus.
func New() *Bus { return &Bus{subs: map[int]sub{}} }

// Publish sends an event to every subscriber whose prefix matches. Subscribers run on the
// caller's goroutine, in no particular order, and must not block.
func (b *Bus) Publish(topic string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		raw = []byte("null")
	}
	ev := module.Event{Topic: topic, Data: raw}
	b.mu.RLock()
	fns := make([]func(module.Event), 0, len(b.subs))
	for _, s := range b.subs {
		if Match(s.prefix, topic) {
			fns = append(fns, s.fn)
		}
	}
	b.mu.RUnlock()
	for _, fn := range fns {
		fn(ev)
	}
}

// Subscribe registers fn for topics under prefix and returns the function that removes it.
func (b *Bus) Subscribe(prefix string, fn func(module.Event)) (stop func()) {
	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = sub{prefix: prefix, fn: fn}
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		delete(b.subs, id)
		b.mu.Unlock()
	}
}

// Match reports whether topic is prefix or lies under it. An empty prefix matches all.
func Match(prefix, topic string) bool {
	return prefix == "" || topic == prefix || strings.HasPrefix(topic, prefix+".")
}

var _ module.Events = (*Bus)(nil)
