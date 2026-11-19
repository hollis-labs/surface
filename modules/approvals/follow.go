package approvals

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The event stream, followed as the owner: GET /api/gate/v1/events with the last seq handled
// as Last-Event-ID, so a reconnect resumes where it stopped and nothing is shown twice. The
// seq is saved after each event is handled; an event at or below it is skipped.

const (
	// idleLimit: the stream sends a keepalive every 15 s; this long without a line means the
	// connection is gone, and it is opened again.
	idleLimit    = 45 * time.Second
	backoffFirst = 2 * time.Second
	backoffLast  = time.Minute
)

// follow keeps the stream open until ctx ends, reconnecting with backoff.
func (m *mod) follow(ctx context.Context) {
	defer close(m.done)
	wait := backoffFirst
	for {
		g, err := m.gate()
		if err == nil {
			started := time.Now()
			err = m.stream(ctx, g)
			if time.Since(started) > time.Minute {
				wait = backoffFirst // it ran a while: the next failure starts the backoff over
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			m.setState(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
			wait = min(wait*2, backoffLast)
		case <-m.wake:
			// Woken (the settings changed, or a reconnect was asked for): try at once, and
			// start the backoff over.
			wait = backoffFirst
		}
	}
}

// stream reads one connection until it ends. It returns why.
func (m *mod) stream(ctx context.Context, g *gate) error {
	key, err := g.key()
	if err != nil {
		return err
	}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.mu.Lock()
	m.cancelStream = cancel
	m.mu.Unlock()
	req, err := http.NewRequestWithContext(sctx, http.MethodGet, g.base+pathEvents, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "text/event-stream")
	if seq := m.cursor(ctx, g.base); seq > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatInt(seq, 10))
	}
	res, err := (&http.Client{CheckRedirect: noRedirect}).Do(req) // no timeout: the stream is long-lived; idleLimit guards it
	if err != nil {
		return fmt.Errorf("approvals: Gatecrasher cannot be reached: %w", redact(err))
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("approvals: the event stream answered %s", res.Status)
	}
	m.setState(nil)
	m.reshow(sctx, g)

	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-sctx.Done():
				return
			}
		}
	}()
	var event, data string
	idle := time.NewTimer(idleLimit)
	defer idle.Stop()
	for {
		select {
		case <-sctx.Done():
			return ctx.Err()
		case <-idle.C:
			return fmt.Errorf("approvals: the event stream was silent for %s", idleLimit)
		case line, ok := <-lines:
			if !ok {
				return fmt.Errorf("approvals: the event stream ended")
			}
			idle.Reset(idleLimit)
			switch {
			case line == "":
				if data != "" && (event == "" || event == "approval") {
					var ev feedEvent
					if json.Unmarshal([]byte(data), &ev) == nil {
						if err := m.handle(sctx, g, ev); err != nil {
							return err // reconnect from the last seq handled, this event included
						}
					}
				}
				event, data = "", ""
			case strings.HasPrefix(line, ":"):
				// keepalive
			case strings.HasPrefix(line, "event:"):
				event = strings.TrimSpace(line[len("event:"):])
			case strings.HasPrefix(line, "data:"):
				data += strings.TrimSpace(line[len("data:"):])
			}
		}
	}
}

// handle acts on one event: a pending approval is shown (once), a closed one's card goes.
// The seq is saved after, so an event is never handled twice across reconnects; one that
// could not be handled ends the connection, and the next starts from it again.
func (m *mod) handle(ctx context.Context, g *gate, ev feedEvent) error {
	if ev.Seq <= m.cursor(ctx, g.base) {
		return nil
	}
	if ev.ApprovalID != "" {
		if ev.Status == "pending" {
			if err := m.show(ctx, g, ev.ApprovalID); err != nil {
				var p permanent
				if !errors.As(err, &p) {
					return fmt.Errorf("approvals: %s was not shown: %w", ev.ApprovalID, err)
				}
				// It will never show as it is: tell the owner, and go on with the rest.
				m.unshowable(ctx, ev.ApprovalID, err)
			}
		} else {
			m.close(ctx, ev.ApprovalID, ev.Status)
		}
	}
	m.setCursor(ctx, g.base, ev.Seq)
	return nil
}
