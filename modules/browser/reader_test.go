package browser

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestReaderSelectsUnmarkedArticleAndPreservesSafeMedia(t *testing.T) {
	base, _ := url.Parse("https://example.com/posts/one")
	prose := strings.Repeat("Article sentences with useful details. ", 30)
	_, text, _ := extract(`<body><nav>Menu <a href="/menu">Menu</a></nav><div><div><h1>Article</h1><p>`+prose+`<a href="../source">Source</a></p><h3>Details</h3><img src="/photo.jpg" alt="Photo"><script>evil()</script><a href="javascript:evil()">Bad</a><img src="data:text/html,bad"><iframe src="https://example.com"></iframe></div><footer>Footer links</footer></div></body>`, base)
	for _, want := range []string{"# Article", "### Details", "[Source](https://example.com/source)", "![Photo](https://example.com/photo.jpg)"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q: %s", want, text)
		}
	}
	for _, bad := range []string{"Menu", "Footer", "evil()", "javascript:", "data:text", "<iframe"} {
		if strings.Contains(text, bad) {
			t.Errorf("unsafe/chrome %q: %s", bad, text)
		}
	}
}
func TestReaderEscapesSourceMarkdown(t *testing.T) {
	base, _ := url.Parse("https://example.com/")
	_, text, _ := extract(`<main><p>[spoof](javascript:bad) &lt;script&gt; # heading</p><img src="/a(b).png" alt="[x]"></main>`, base)
	if strings.Contains(text, "[spoof]") || strings.Contains(text, "<script>") || !strings.Contains(text, "a%28b%29.png") {
		t.Fatal(text)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSearchPageUsesHTMLSearchAndSafeResults(t *testing.T) {
	f := newFetcher()
	calls := 0
	f.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://html.duckduckgo.com/html/" || r.Method != "POST" {
			t.Fatalf("request: %s %s", r.Method, r.URL)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "q=surface+docs" {
			t.Fatal(string(body))
		}
		doc := `<div class="result"><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fdocs">Docs</a><span class="result__snippet">Useful docs</span></div><a class="result__a" href="javascript:bad">Bad</a><a class="result__a" href="https://example.com/docs">Duplicate</a>`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(doc)), Header: make(http.Header), Request: r}, nil
	})
	p, err := f.get(context.Background(), "https://duckduckgo.com/?q=surface+docs")
	if err != nil || !p.Search || p.Query != "surface docs" || len(p.Results) != 1 || p.Results[0].URL != "https://example.com/docs" || p.Results[0].Snippet != "Useful docs" {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = f.get(context.Background(), "https://duckduckgo.com/")
	if err != nil || !p.Search || calls != 1 {
		t.Fatalf("home: %+v %v calls%d", p, err, calls)
	}
	if _, ok := searchQuery("https://example.com/?q=test"); ok {
		t.Fatal("ordinary page treated as search")
	}
}
func TestSearchChallengeReturnsAnError(t *testing.T) {
	f := newFetcher()
	f.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 202, Status: "202 Accepted", Body: io.NopCloser(strings.NewReader("challenge")), Request: r}, nil
	})
	if _, err := f.search(context.Background(), "query"); err == nil {
		t.Fatal("challenge returned as empty success")
	}
}
