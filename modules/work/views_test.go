package work

import "testing"

func TestDescribeWindow(t *testing.T) {
	cases := []struct {
		args       map[string]any
		kind, ref  string
		item, view string
	}{
		{map[string]any{}, "workspace", "work", "", ""},
		{map[string]any{"workspace": "Bad Slug"}, "workspace", "work", "", ""},
		{map[string]any{"workspace": "portfolio", "view": "v1"}, "workspace", "work:portfolio", "", "v1"},
		{map[string]any{"workspace": "portfolio", "item": "ab12cd34"}, "item", "work:portfolio/ab12cd34", "ab12cd34", ""},
		{map[string]any{"workspace": "portfolio", "item": "../x"}, "workspace", "work:portfolio", "", ""},
	}
	for _, c := range cases {
		wc := describeWindow("planner", c.args)
		if wc.Kind != c.kind || wc.Ref != c.ref {
			t.Errorf("%v: got %s %s, want %s %s", c.args, wc.Kind, wc.Ref, c.kind, c.ref)
		}
		if got, _ := wc.Extra["item"].(string); got != c.item {
			t.Errorf("%v: item %q, want %q", c.args, got, c.item)
		}
		if got, _ := wc.Extra["view"].(string); got != c.view {
			t.Errorf("%v: view %q, want %q", c.args, got, c.view)
		}
		if wc.Extra["app"] != "planner" {
			t.Errorf("%v: app %v", c.args, wc.Extra["app"])
		}
	}
}
