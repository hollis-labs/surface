package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/hollis-labs/libs/agent/chat"
	chatstream "github.com/hollis-labs/libs/ui/ui-go/chatstream"
	"github.com/hollis-labs/nanite/chatrunner"
)

// A formerly hosted conversation gets a fresh Nanite history even if this same chat
// used Nanite before. The chat ID and its complete transcript do not change. The
// runner reference kept by chat persists the mapping across app restarts.
const hostUpgradePrefix = "surface-host-upgrade-"
const historySeed = `[A note from the app, not the owner: this conversation went on before your history here begins. The earlier messages are not in your history; they are in the conversation's transcript, which the owner can scroll back through. What carries over is in the context block: your notes on each slot and the slot's standing instructions. Pick up from those, and ask the owner if you need something from before.]`

func originalChatID(id string) string { return strings.TrimPrefix(id, hostUpgradePrefix) }

func (w *wire) naniteChatID(ctx context.Context, id string) (string, error) {
	c, err := w.store.Get(ctx, id)
	if errors.Is(err, chat.ErrNotFound) {
		return id, nil
	}
	if err != nil {
		return "", err
	}
	upgraded := hostUpgradePrefix + id
	if strings.HasPrefix(c.RunnerRef, "thr_") || c.RunnerRef == chatrunner.ConversationID(upgraded) {
		return upgraded, nil
	}
	return id, nil
}

type naniteWireRunner struct {
	*chatrunner.Runner
	wire *wire
}

func (r naniteWireRunner) Info(ctx context.Context, id string) (chat.Info, error) {
	mapped, err := r.wire.naniteChatID(ctx, id)
	if err != nil {
		return chat.Info{}, err
	}
	return r.Runner.Info(ctx, mapped)
}

func (r naniteWireRunner) prepare(ctx context.Context, turn chat.Turn) (chat.Turn, error) {
	original := turn.ConversationID
	id, err := r.wire.naniteChatID(ctx, original)
	if err != nil {
		return turn, err
	}
	c, err := r.Agent().EnsureConversation(ctx, chatrunner.ConversationID(id))
	if err != nil {
		return turn, err
	}
	turn.ConversationID = id
	history, err := c.History(ctx)
	if err != nil {
		return turn, err
	}
	if len(history) > 0 {
		return turn, nil
	}
	// No model history yet: seed only when there is an earlier transcript.
	prior := id != original // a legacy thread may predate chat run records
	if !prior {
		prior, err = r.wire.ranBefore(ctx, original)
		if err != nil {
			return turn, err
		}
	}
	if prior {
		turn.Text = historySeed + "\n\n" + turn.Text
	}
	return turn, nil
}

func (r naniteWireRunner) Start(ctx context.Context, turn chat.Turn) (chat.Run, error) {
	turn, err := r.prepare(ctx, turn)
	if err != nil {
		return nil, err
	}
	return r.Runner.Start(ctx, turn)
}

func (r naniteWireRunner) Record(ctx context.Context, turn chat.Turn, answer string) error {
	turn, err := r.prepare(ctx, turn)
	if err != nil {
		return err
	}
	return r.Runner.Record(ctx, turn, answer)
}

func (w *wire) ranBefore(ctx context.Context, id string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for rec, err := range w.store.After(ctx, id, 0) {
		if err != nil {
			return false, err
		}
		if rec.Name != string(chatstream.VerbRunStart) {
			continue
		}
		var start struct {
			Provider string `json:"provider"`
		}
		if err := json.Unmarshal(rec.Data, &start); err != nil {
			return false, err
		}
		if start.Provider != chat.ProviderHost {
			return true, nil
		}
	}
	return false, nil
}
