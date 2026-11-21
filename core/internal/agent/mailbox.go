package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hollis-labs/libs/agent/agentdir"
	"github.com/hollis-labs/libs/agent/chat"
	nanite "github.com/hollis-labs/nanite/agent"
	"github.com/hollis-labs/nanite/chatrunner"
)

// The agent's own Tether mailbox (Nanite's chatrunner mailbox). The agent directory's
// host.yaml names the address (identity.tether); the agent config's mailbox section names
// Tether and the credential file. Nanite polls the address's unread mail, oldest first,
// hands each message to the agent as a notice turn, and marks it read once handled (a
// message that fails three times is parked and marked read).
//
// The mail turns keep a Nanite history of their own (mailboxChat), so mail is never mixed
// into the owner's conversation history and survives the owner starting a new conversation.
// They are shown in the conversation the owner has open, labeled as Tether mail.
//
// Nothing else in Surface reads the agent's own address on its own: the tether module's mail
// tools read it only when the model calls them, and the assist and inbox modules read the
// owner's addresses (assist.addresses, inbox.addresses). Do not add the agent's address to
// those settings while the mailbox is on, or the same mail is handled twice.

// mailboxChat is the runner's chat for the agent's own mail. It is not a wire conversation:
// its runs are shown in the conversation in front (mailboxDisplay).
const mailboxChat = "surface-mailbox"

// mailboxLabel is how a mail turn is labeled in the conversation.
const mailboxLabel = "Tether mail"

// naniteMailbox is the runner options for the agent's mailbox: none when the agent config has
// no mailbox section. host.yaml's identity.tether and the config's mailbox go together.
func (s *Service) naniteMailbox(dir string) (*nanite.TetherConfig, *chatrunner.MailboxOptions, error) {
	snapshot, err := agentdir.Load(dir)
	if err != nil {
		return nil, nil, err
	}
	identity := snapshot.Profile.Identity != nil && snapshot.Profile.Identity.Tether != ""
	m := s.cfg.Mailbox
	switch {
	case m == nil && identity:
		return nil, nil, fmt.Errorf("agent: host.yaml in %s sets identity.tether; set mailbox.url and mailbox.credential_file in %s", dir, ConfigPath(s.k.AppName()))
	case m == nil:
		return nil, nil, nil
	case !identity:
		s.log.Warn("the agent config has a mailbox but host.yaml sets no identity.tether; the mailbox is off", "dir", dir)
		return nil, nil, nil
	}
	tether := &nanite.TetherConfig{Endpoint: m.URL, CredentialFile: m.CredentialFile}
	opts := &chatrunner.MailboxOptions{ConversationID: mailboxChat, PollInterval: time.Duration(m.PollSeconds) * time.Second}
	return tether, opts, nil
}

// mailboxDisplay is where a mail run is shown: the conversation in front, with the turn
// labeled. Any other chat is shown where it is.
func (w *wire) mailboxDisplay(ctx context.Context, id string, turn chat.Turn) (string, chat.Turn, error) {
	if id != mailboxChat {
		return id, turn, nil
	}
	view, err := w.svc.EnsureConversation(ctx, w.agent)
	if err != nil {
		return "", turn, err
	}
	meta := map[string]any{}
	if len(turn.Meta) > 0 {
		if err = json.Unmarshal(turn.Meta, &meta); err != nil {
			return "", turn, err
		}
	}
	meta["auto"] = mailboxLabel
	if turn.Meta, err = json.Marshal(meta); err != nil {
		return "", turn, err
	}
	return view.ConversationID, turn, nil
}
