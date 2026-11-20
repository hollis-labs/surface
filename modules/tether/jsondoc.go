package tether

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// A document whose text is JSON (a Caster doc written by a tool, say) is summarized for the
// preview card, never shown as a fragment of its text. The page draws the whole of it as
// fields in the record window (web/src/cite/structured.ts).

// looksLikeJSON reports whether text starts the way a JSON object or array does.
func looksLikeJSON(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

// parseJSONDoc parses text that is a JSON object or array. Anything else (a scalar, Markdown,
// JSON cut short) is not a JSON document.
func parseJSONDoc(s string) (any, bool) {
	if !looksLikeJSON(s) {
		return nil, false
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, false
	}
	return v, true
}

// summaryKeys are the fields that say what a document is, in the order they are preferred.
var summaryKeys = []string{"summary", "description", "status", "state", "message"}

// summaryOfJSON is a short plain-text account of a JSON document: what it says it is (its
// summary, description or status), then how many of each list it holds ("3 messages").
func summaryOfJSON(v any) string {
	var lines []string
	switch d := v.(type) {
	case map[string]any:
		for _, k := range summaryKeys {
			if s, ok := d[k].(string); ok && strings.TrimSpace(s) != "" {
				lines = append(lines, excerpt(firstParagraph(s), 200))
				break
			}
		}
		keys := make([]string, 0, len(d))
		for k := range d {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var counts []string
		for _, k := range keys {
			if a, ok := d[k].([]any); ok {
				counts = append(counts, count(len(a), humanKey(k)))
			}
		}
		if len(counts) > 0 {
			lines = append(lines, strings.Join(counts, " · "))
		}
		if len(lines) == 0 {
			names := make([]string, 0, 4)
			for i, k := range keys {
				if i == 4 {
					names = append(names, "…")
					break
				}
				names = append(names, humanKey(k))
			}
			lines = append(lines, count(len(keys), "fields")+": "+strings.Join(names, ", "))
		}
	case []any:
		lines = append(lines, count(len(d), "items"))
	}
	return strings.Join(lines, "\n")
}

// count is "1 message", "3 messages": the noun is given in the plural, as a JSON key usually is.
func count(n int, plural string) string {
	if n == 1 && strings.HasSuffix(plural, "s") && !strings.HasSuffix(plural, "ss") {
		return fmt.Sprintf("1 %s", strings.TrimSuffix(plural, "s"))
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// humanKey is a JSON key as words: "open_items" is "open items".
func humanKey(k string) string {
	return strings.TrimSpace(strings.NewReplacer("_", " ", "-", " ").Replace(k))
}

func firstParagraph(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "\n\n"); i >= 0 {
		return s[:i]
	}
	return s
}
