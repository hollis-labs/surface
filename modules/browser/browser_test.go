package browser

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestExtract(t *testing.T) {
	base, _ := url.Parse("https://example.com/a/")
	title, text, links := extract(`<html><head><title> Hi </title><script>evil()</script></head>
<body><h1>Head</h1><p>One <b>two</b></p><ul><li>x</li></ul><a href="../b#frag">B link</a><a href="javascript:x">no</a></body></html>`, base)
	if title != "Hi" || strings.Contains(text, "evil") || !strings.Contains(text, "# Head") || !strings.Contains(text, "One **two**") || !strings.Contains(text, "- x") {
		t.Fatalf("%q %q", title, text)
	}
	if len(links) != 1 || links[0].URL != "https://example.com/b" {
		t.Fatalf("%+v", links)
	}
}

func TestNormalizeAndNavigate(t *testing.T) {
	for in, want := range map[string]string{
		"example.com":        "https://example.com",
		"http://x.org/p":     "http://x.org/p",
		"surface ade design": searchURL + "surface+ade+design",
	} {
		if got, _ := normalize(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
	if _, err := normalize("file:///etc/passwd"); err == nil {
		t.Error("file: allowed")
	}
	a := navigate(map[string]any{"url": "a", "history": []any{"a", "b", "c"}, "at": float64(1)}, "d")
	if h := a["history"].([]any); len(h) != 3 || h[2] != "d" || a["at"] != 2 {
		t.Fatalf("%+v", a)
	}
	h := http.Header{}
	h.Set("X-Frame-Options", "DENY")
	if framable(h) {
		t.Error("DENY framable")
	}
}

func TestReaderPrefersMain(t *testing.T) {
	base, _ := url.Parse("https://example.com/")
	long := strings.Repeat("Real words about the subject. ", 30)
	_, text, links := extract(`<html><body><nav><a href="/a">Menu A</a> Skip to content</nav>
<main><h1>Title</h1><p>`+long+`</p></main><footer><a href="/f">Footer</a></footer></body></html>`, base)
	if strings.Contains(text, "Skip to content") || strings.Contains(text, "Footer") || !strings.Contains(text, "# Title") {
		t.Fatalf("%q", text[:120])
	}
	if len(links) != 2 {
		t.Fatalf("links from the whole page: %+v", links)
	}
}
