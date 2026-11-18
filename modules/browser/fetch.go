package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// Page is a web page as the server read it: its words and its links, without its scripts.
type Page struct {
	URL      string `json:"url"`
	FinalURL string `json:"final_url"`
	Title    string `json:"title"`
	Text     string `json:"text"`
	Links    []Link `json:"links,omitempty"`
	// Framable says whether the site lets another page show it in a frame.
	Framable bool   `json:"framable"`
	Fetched  string `json:"fetched"`
}

// Link is one link on a page.
type Link struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

const (
	maxPage   = 6 << 20
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15 Surface/0.1"
	cacheTTL  = 5 * time.Minute
)

// normalize makes what the owner typed into a URL: a bare host gets https, words become a search.
func normalize(in string) (string, error) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", errors.New("no address")
	}
	if !strings.Contains(in, "://") {
		if strings.Contains(in, " ") || !strings.Contains(in, ".") {
			return searchURL + url.QueryEscape(in), nil
		}
		in = "https://" + in
	}
	u, err := url.Parse(in)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("only http and https pages can be opened, not %s", u.Scheme)
	}
	return u.String(), nil
}

type fetcher struct {
	client *http.Client
	mu     sync.Mutex
	cache  map[string]cached
}

type cached struct {
	page Page
	at   time.Time
}

func newFetcher() *fetcher {
	return &fetcher{client: &http.Client{Timeout: 20 * time.Second}, cache: map[string]cached{}}
}

// cachedPage returns a page read in the last few minutes, without fetching.
func (f *fetcher) cachedPage(u string) (Page, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cache[u]
	if !ok || time.Since(c.at) > cacheTTL {
		return Page{}, false
	}
	return c.page, true
}

func (f *fetcher) get(ctx context.Context, u string) (Page, error) {
	if p, ok := f.cachedPage(u); ok {
		return p, nil
	}
	// Fetching the address the owner or the agent gave is the module's purpose; the app is
	// single-user and loopback-only.
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil) //nolint:gosec // see above
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.5")
	req.Header.Set("Accept-Language", "en")
	resp, err := f.client.Do(req) //nolint:gosec // see above
	if err != nil {
		return Page{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return Page{}, fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPage))
	if err != nil {
		return Page{}, err
	}
	p := Page{URL: u, FinalURL: resp.Request.URL.String(), Framable: framable(resp.Header), Fetched: time.Now().UTC().Format(time.RFC3339)}
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "html") || ct == "" {
		p.Title, p.Text, p.Links = extract(string(raw), resp.Request.URL)
	} else if strings.HasPrefix(ct, "text/") || strings.Contains(ct, "json") {
		p.Text = string(raw)
	} else {
		p.Text = fmt.Sprintf("(%s, %d bytes: not a page that can be read as text)", ct, len(raw))
	}
	if p.Title == "" {
		p.Title = resp.Request.URL.Host
	}
	f.mu.Lock()
	if len(f.cache) > 200 {
		f.cache = map[string]cached{}
	}
	f.cache[u] = cached{p, time.Now()}
	f.mu.Unlock()
	return p, nil
}

// framable reads X-Frame-Options and CSP frame-ancestors.
func framable(h http.Header) bool {
	if x := strings.ToLower(h.Get("X-Frame-Options")); x == "deny" || x == "sameorigin" {
		return false
	}
	for _, csp := range h.Values("Content-Security-Policy") {
		for _, d := range strings.Split(csp, ";") {
			d = strings.TrimSpace(strings.ToLower(d))
			if v, ok := strings.CutPrefix(d, "frame-ancestors"); ok {
				v = strings.TrimSpace(v)
				return v == "*" || strings.Contains(v, " *") || strings.HasPrefix(v, "* ")
			}
		}
	}
	return true
}

var skip = map[string]bool{"script": true, "style": true, "noscript": true, "svg": true, "template": true, "iframe": true, "head": true}

// chrome is the site around the content: left out of the reader text, read for links.
var chrome = map[string]bool{"nav": true, "aside": true, "form": true, "button": true}
var block = map[string]bool{"p": true, "div": true, "br": true, "li": true, "tr": true, "section": true, "article": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "pre": true, "blockquote": true, "header": true, "footer": true, "table": true, "ul": true, "ol": true}

// extract reads a page's title, its text (blocks on their own lines, headings marked) and links.
func extract(doc string, base *url.URL) (title, text string, links []Link) {
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return "", doc, nil
	}
	var b strings.Builder
	seen := map[string]bool{}
	textOnly := false
	var walk func(n *html.Node, pre bool)
	walk = func(n *html.Node, pre bool) {
		if n.Type == html.ElementNode {
			if n.Data == "title" && title == "" && n.FirstChild != nil {
				title = strings.TrimSpace(n.FirstChild.Data)
				return
			}
			if textOnly && chrome[n.Data] {
				return
			}
			if skip[n.Data] {
				// The title lives in head; read it and skip the rest.
				if n.Data == "head" {
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.ElementNode && c.Data == "title" {
							walk(c, false)
						}
					}
				}
				return
			}
			if block[n.Data] {
				b.WriteString("\n")
				if len(n.Data) == 2 && n.Data[0] == 'h' {
					b.WriteString(strings.Repeat("#", int(n.Data[1]-'0')) + " ")
				}
				if n.Data == "li" {
					b.WriteString("- ")
				}
			}
			if n.Data == "pre" {
				pre = true
			}
			if n.Data == "a" {
				var href string
				for _, a := range n.Attr {
					if a.Key == "href" {
						href = a.Val
					}
				}
				if u, err := base.Parse(href); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
					u.Fragment = ""
					t := strings.Join(strings.Fields(textOf(n)), " ")
					if t != "" && !seen[u.String()] && len(links) < 300 {
						seen[u.String()] = true
						links = append(links, Link{Text: clip(t, 120), URL: u.String()})
					}
				}
			}
		}
		if n.Type == html.TextNode {
			if pre {
				b.WriteString(n.Data)
			} else if t := strings.Join(strings.Fields(n.Data), " "); t != "" {
				if s := b.String(); len(s) > 0 && !strings.HasSuffix(s, "\n") && !strings.HasSuffix(s, " ") {
					b.WriteString(" ")
				}
				b.WriteString(t)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, pre)
		}
		if n.Type == html.ElementNode && block[n.Data] {
			b.WriteString("\n")
		}
	}
	// The whole page, for its title and links; then, for the text, its main content when it
	// marks one (main, else the largest article), so the reader skips the site's chrome.
	walk(root, false)
	b.Reset()
	textOnly = true
	if content := mainContent(root); content != nil {
		walk(content, false)
	} else {
		walk(root, false)
	}
	// Collapse runs of blank lines.
	var out []string
	blank := 0
	for _, l := range strings.Split(b.String(), "\n") {
		l = strings.TrimRight(l, " \t")
		if strings.TrimSpace(l) == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return title, strings.TrimSpace(strings.Join(out, "\n")), links
}

// mainContent is the page's main element, else its largest article, when it holds a fair
// amount of text; nil means use the whole page.
func mainContent(root *html.Node) *html.Node {
	var main, best *html.Node
	bestLen := 0
	var find func(n *html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch {
			case n.Data == "main" || attr(n, "role") == "main":
				if main == nil {
					main = n
				}
			case n.Data == "article":
				if l := len(strings.Join(strings.Fields(textOf(n)), " ")); l > bestLen {
					best, bestLen = n, l
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(root)
	const enough = 400
	if main != nil && len(strings.Join(strings.Fields(textOf(main)), " ")) >= enough {
		return main
	}
	if best != nil && bestLen >= enough {
		return best
	}
	return nil
}

func textOf(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(textOf(c) + " ")
	}
	return b.String()
}

// Result is one search result.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

// search asks DuckDuckGo's HTML page and reads its results.
func (f *fetcher) search(ctx context.Context, q string) ([]Result, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://html.duckduckgo.com/html/", strings.NewReader(url.Values{"q": {q}}.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("search answered %s", resp.Status)
	}
	root, err := html.Parse(io.LimitReader(resp.Body, maxPage))
	if err != nil {
		return nil, err
	}
	var out []Result
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "result__a") {
			r := Result{Title: strings.Join(strings.Fields(textOf(n)), " "), URL: unwrap(attr(n, "href"))}
			out = append(out, r)
		}
		if n.Type == html.ElementNode && hasClass(n, "result__snippet") && len(out) > 0 && out[len(out)-1].Snippet == "" {
			out[len(out)-1].Snippet = strings.Join(strings.Fields(textOf(n)), " ")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if len(out) > 15 {
		out = out[:15]
	}
	return out, nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, c string) bool {
	for _, f := range strings.Fields(attr(n, "class")) {
		if f == c {
			return true
		}
	}
	return false
}

// unwrap takes the target out of DuckDuckGo's redirect link.
func unwrap(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	if t := u.Query().Get("uddg"); t != "" {
		return t
	}
	return href
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
