package tetherlink

import (
	"encoding/json"
	"errors"
	"testing"
)

// A result the gateway cut short is two text blocks; the note is set aside and the tool's JSON
// is what is returned.
func TestToolTextSetsTheGatewayNoteAside(t *testing.T) {
	raw := json.RawMessage(`{"content":[{"type":"text","text":"{\"messages\":[{\"id\":\"m1\"}]}"},{"type":"text","text":"{\"gateway_truncated\":{\"truncated\":true,\"next_cursor\":\"c.15\"}}"}]}`)
	text, err := ToolText(raw)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Messages []struct{ ID string } `json:"messages"`
	}
	if err := json.Unmarshal([]byte(text), &v); err != nil || len(v.Messages) != 1 {
		t.Fatalf("%q: %v", text, err)
	}
	if _, err := ToolText(json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"secret detail"}]}`)); !errors.Is(err, ErrRefused) {
		t.Fatalf("a refusal: %v", err)
	}
}
