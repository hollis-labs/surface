package tether

import (
	"fmt"
	"strings"
)

// guard adjusts a call's arguments to what the agent may do, or refuses it. urn is the agent's
// own address (set for the mail tools only); author is who its Tesseract writes are recorded as.
func guard(tool string, args map[string]any, urn, author string) error {
	switch tool {
	case "memory_write", "knowledge_write", "workspace_write":
		// The owner's own scope is theirs to write; the agent writes as itself, elsewhere.
		ns, _ := args["namespace"].(string)
		ns = strings.ToLower(strings.TrimSpace(ns))
		if ns == "system" || strings.HasPrefix(ns, "system/") || ns == "user" || strings.HasPrefix(ns, "user/") {
			return fmt.Errorf("the agent does not write to %q: that scope is the owner's own. Use a project or app namespace (for example app/surface/...), or tell the owner what to record", ns)
		}
		if actor, _ := args["actor"].(string); strings.EqualFold(actor, "user") {
			delete(args, "actor")
		}
		if author = strings.TrimSpace(author); author == "" {
			author = "surface"
		}
		args["author_agent_id"] = author
	case "tether_message_notify":
		args["from"] = urn
	case "tether_message_list":
		args["to"] = urn
	case "tether_message_get", "tether_message_mark_read":
		args["as"] = urn
	}
	return nil
}
