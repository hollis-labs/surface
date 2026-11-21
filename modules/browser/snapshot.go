package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // raster decoders; never SVG
	_ "image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const snapshotCSP = "default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src 'none'; connect-src 'none'; frame-src 'none'; object-src 'none'; media-src 'none'; base-uri 'none'; form-action 'none'"

// Snapshot has no persistent cache. HTML is consumed only by an opaque sandboxed frame.
type Snapshot struct {
	URL      string `json:"url"`
	FinalURL string `json:"final_url"`
	Title    string `json:"title"`
	HTML     string `json:"html"`
	Text     string `json:"text"`
	Links    []Link `json:"links"`
	Fetched  string `json:"fetched"`
	Missing  int    `json:"missing_assets"`
	Dropped  int    `json:"dropped_css"`
}

type snapshotBuilder struct {
	ctx              context.Context
	client           *http.Client
	result           Snapshot
	bytes, resources int
	imageBytes       int
	pixels           int64
	text             strings.Builder
	images           map[string]string
	imports          map[string]bool
	output           boundedBuffer
}

// boundedBuffer caps both serialization and raster re-encoding before allocation grows.
type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("snapshot output limit exceeded")
	}
	return b.Buffer.Write(p)
}
func (b *boundedBuffer) WriteString(p string) (int, error) { return b.Write([]byte(p)) }

// withSnapshot holds its reservation through serialization and HTTP response delivery.
func (m *mod) withSnapshot(ctx context.Context, rawURL string, consume func(Snapshot) error) error {
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	select {
	case m.snapshots <- struct{}{}:
		defer func() { <-m.snapshots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	result, err := m.buildSnapshot(ctx, rawURL)
	if err != nil {
		return err
	}
	return consume(result)
}
func (m *mod) snapshot(ctx context.Context, rawURL string) (result Snapshot, err error) {
	err = m.withSnapshot(ctx, rawURL, func(p Snapshot) error { result = p; return nil })
	return result, err
}
func (m *mod) buildSnapshot(ctx context.Context, rawURL string) (Snapshot, error) {
	b := &snapshotBuilder{ctx: ctx, client: m.public.client, images: map[string]string{}, imports: map[string]bool{}, output: boundedBuffer{limit: maxSerializedBytes}}
	raw, base, err := b.fetch(rawURL, maxHTML, "text/html")
	if err != nil {
		return Snapshot{}, err
	}
	root, err := snapshotDOM(ctx, raw)
	if err != nil {
		return Snapshot{}, err
	}
	b.result = Snapshot{URL: rawURL, FinalURL: base.String(), Fetched: time.Now().UTC().Format(time.RFC3339), Links: []Link{}}
	// The first valid base is resolution-only; it is never emitted.
	var inspect func(*html.Node)
	baseSeen := false
	inspect = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Namespace == "" {
			if n.Data == "base" && !baseSeen {
				baseSeen = true
				if u := snapshotURL(base, attr(n, "href")); u != nil {
					base = u
				}
			}
			if n.Data == "title" && b.result.Title == "" {
				b.result.Title = clip(strings.TrimSpace(textOf(n)), 300)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			inspect(c)
		}
	}
	inspect(root)
	if b.result.Title == "" {
		b.result.Title = base.Hostname()
	}
	if err = b.write(`<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="` + html.EscapeString(snapshotCSP) + `"><meta name="referrer" content="no-referrer"></head><body>`); err != nil {
		return Snapshot{}, err
	}
	if err = b.render(root, base); err != nil {
		return Snapshot{}, err
	}
	if err = b.write("</body></html>"); err != nil {
		return Snapshot{}, err
	}
	b.result.HTML = b.output.String()
	b.result.Text = b.text.String()
	return b.result, nil
}

func snapshotDOM(ctx context.Context, raw []byte) (*html.Node, error) {
	// Tokenize before the tree builder to cap pathological depth/node growth.
	z := html.NewTokenizer(bytes.NewReader(raw))
	depth, nodes := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tt := z.Next()
		if tt == html.ErrorToken {
			if !errors.Is(z.Err(), io.EOF) {
				return nil, errors.New("invalid snapshot HTML")
			}
			break
		}
		nodes++
		if tt == html.StartTagToken {
			name, _ := z.TagName()
			if !voidHTML[string(name)] {
				depth++
			}
		}
		if tt == html.EndTagToken && depth > 0 {
			depth--
		}
		if nodes > maxSnapshotNodes || depth > maxSnapshotDepth {
			return nil, errors.New("snapshot HTML complexity limit exceeded")
		}
	}
	root, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	nodes = 0
	var check func(*html.Node, int) error
	check = func(n *html.Node, d int) error {
		nodes++
		if nodes > maxSnapshotNodes || d > maxSnapshotDepth {
			return errors.New("snapshot DOM limit exceeded")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := check(c, d+1); err != nil {
				return err
			}
		}
		return nil
	}
	return root, check(root, 0)
}

func snapshotURL(base *url.URL, raw string) *url.URL {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil
	}
	u = base.ResolveReference(u)
	u.Fragment = ""
	if validFetchURL(u, true) != nil {
		return nil
	}
	return u
}

func (b *snapshotBuilder) fetch(raw string, limit int64, want string) ([]byte, *url.URL, error) {
	if b.resources >= maxResources {
		return nil, nil, errors.New("snapshot resource limit exceeded")
	}
	b.resources++
	u, err := url.Parse(raw)
	if err != nil || validFetchURL(u, true) != nil {
		return nil, nil, errPrivateTarget
	}
	ctx, cancel := context.WithTimeout(b.ctx, resourceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil) //nolint:gosec // publicTransport validates every DNS answer and pins all connections, including redirects
	if err != nil {
		return nil, nil, errors.New("invalid snapshot URL")
	}
	req.Header.Set("Accept", want)
	resp, err := b.client.Do(req) //nolint:gosec // only the guarded public client is supplied, never the owner client
	if err != nil {
		return nil, nil, fetchError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("snapshot resource answered HTTP %d", resp.StatusCode)
	}
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if (want == "image/*" && !strings.HasPrefix(ct, "image/")) || (want != "image/*" && ct != want) {
		return nil, nil, errors.New("unsupported snapshot content type")
	}
	left := int64(maxSnapshotBytes - b.bytes)
	if left < limit {
		limit = left
	}
	if limit <= 0 {
		return nil, nil, errors.New("snapshot total byte limit exceeded")
	}
	// Charge the full allowance on failure as even a rejected body consumed bytes.
	b.bytes += int(limit)
	data, err := responseBytes(resp, limit)
	if err != nil {
		return nil, nil, err
	}
	b.bytes -= int(limit) - len(data)
	return data, resp.Request.URL, nil
}

func (b *snapshotBuilder) raster(base *url.URL, raw string) string {
	u := snapshotURL(base, raw)
	if u == nil {
		b.result.Missing++
		return ""
	}
	key := u.String()
	if s, ok := b.images[key]; ok {
		return s
	}
	b.images[key] = ""
	data, _, err := b.fetch(key, maxImage, "image/*")
	if err != nil {
		b.result.Missing++
		return ""
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		b.result.Missing++
		return ""
	}
	b.pixels += int64(cfg.Width) * int64(cfg.Height)
	if b.pixels > maxSnapshotPixels {
		b.result.Missing++
		return ""
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		b.result.Missing++
		return ""
	}
	out := boundedBuffer{limit: maxImage}
	if err = png.Encode(&out, img); err != nil {
		b.result.Missing++
		return ""
	}
	s := "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes())
	if b.imageBytes+len(s) > maxRetainedAssetBytes {
		b.result.Missing++
		return ""
	}
	b.imageBytes += len(s)
	b.images[key] = s
	return s
}

var voidHTML = words("area base br col embed hr img input link meta param source track wbr")
var staticHTML = words("a abbr address article aside b bdi bdo blockquote br caption center cite code col colgroup dd del details dfn div dl dt em figcaption figure footer h1 h2 h3 h4 h5 h6 header hgroup hr i img ins kbd li main mark nav ol p pre q rp rt ruby s samp section small span strong sub summary sup table tbody td th thead time tr u ul var wbr")
var dropHTML = words("script noscript template iframe frame frameset object embed svg math form input button select textarea option audio video source track canvas title base meta")

func words(s string) map[string]bool {
	m := map[string]bool{}
	for _, v := range strings.Fields(s) {
		m[v] = true
	}
	return m
}
func (b *snapshotBuilder) write(s string) error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	_, err := b.output.WriteString(s)
	return err
}
func (b *snapshotBuilder) render(n *html.Node, base *url.URL) error {
	if n.Type == html.TextNode {
		if b.text.Len() < maxSnapshotText {
			text := n.Data
			remaining := maxSnapshotText - b.text.Len()
			if len(text) > remaining {
				text = text[:remaining]
			}
			b.text.WriteString(text)
			if b.text.Len() < maxSnapshotText {
				b.text.WriteByte(' ')
			}
		}
		return b.write(html.EscapeString(n.Data))
	}
	if n.Type == html.ElementNode {
		if n.Namespace != "" || dropHTML[n.Data] {
			return nil
		}
		if n.Data == "style" {
			return b.style(textOf(n), base, 0)
		}
		if n.Data == "link" {
			if strings.EqualFold(attr(n, "rel"), "stylesheet") {
				return b.writeStyle(b.importSheet(base, attr(n, "href"), 0))
			}
			return nil
		}
		if staticHTML[n.Data] {
			if n.Data == "a" && len(b.result.Links) < maxSnapshotLinks {
				if u := snapshotURL(base, attr(n, "href")); u != nil {
					found := false
					for _, existing := range b.result.Links {
						if existing.URL == u.String() {
							found = true
							break
						}
					}
					if !found {
						b.result.Links = append(b.result.Links, Link{Text: clip(strings.Join(strings.Fields(textOf(n)), " "), 120), URL: u.String()})
					}
				}
			}
			tag := "<" + n.Data
			for _, a := range n.Attr {
				if a.Namespace != "" {
					continue
				}
				v := a.Val
				switch a.Key {
				case "id", "class", "title", "lang", "dir", "alt", "width", "height", "colspan", "rowspan":
				case "style":
					v = b.css(v, base, true, 0)
				default:
					continue
				}
				tag += " " + a.Key + `="` + html.EscapeString(v) + `"`
			}
			if n.Data == "img" {
				if src := b.raster(base, attr(n, "src")); src != "" {
					tag += ` src="` + src + `"`
				} else {
					tag += ` alt="Image unavailable"`
				}
			}
			if err := b.write(tag + ">"); err != nil {
				return err
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if err := b.render(c, base); err != nil {
					return err
				}
			}
			if !voidHTML[n.Data] {
				return b.write("</" + n.Data + ">")
			}
			return nil
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := b.render(c, base); err != nil {
			return err
		}
	}
	return nil
}

func (b *snapshotBuilder) style(raw string, base *url.URL, depth int) error {
	return b.writeStyle(b.css(raw, base, false, depth))
}
func (b *snapshotBuilder) writeStyle(safe string) error {
	// Raw-text HTML serialization needs its own boundary, independently of CSS parsing.
	safe = strings.ReplaceAll(safe, "<", `\3c `)
	return b.write("<style>" + safe + "</style>")
}

func (b *snapshotBuilder) importSheet(base *url.URL, raw string, depth int) string {
	u := snapshotURL(base, raw)
	if u == nil || depth > maxImportDepth {
		b.result.Missing++
		return ""
	}
	if b.imports[u.String()] {
		b.result.Dropped++
		return ""
	}
	b.imports[u.String()] = true
	data, final, err := b.fetch(u.String(), maxCSS, "text/css")
	if err != nil {
		b.result.Missing++
		return ""
	}
	return b.css(string(data), final, false, depth)
}
