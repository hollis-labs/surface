package browser

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

type snapshotFixture struct{ body, mime string }

func snapshotMod(t *testing.T, fixtures map[string]snapshotFixture) (*mod, *[]string) {
	t.Helper()
	calls := []string{}
	m := &mod{public: newGuardedFetcher(), snapshots: make(chan struct{}, maxSnapshotConcurrency)}
	m.public.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.String())
		f, ok := fixtures[r.URL.String()]
		if !ok {
			return nil, errors.New("missing fixture")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {f.mime}}, Body: io.NopCloser(strings.NewReader(f.body)), Request: r}, nil
	})
	return m, &calls
}
func tinyPNG(t *testing.T) string {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestSnapshotSanitizesHTMLAndCSS(t *testing.T) {
	fixtures := map[string]snapshotFixture{
		"https://example.com/":                {`<html><head><title>Fixture</title><base href="/assets/"><meta http-equiv="refresh" content="0;url=https://example.org"><link rel="preload" href="/leak"><link rel="stylesheet" href="main.css"><style>.safe{color:red;--x:url(https://example.org);width:var(--x);padding:4px;background:url(pic.png)} .escaped{background:u\72l(https://example.org);color:blue}</style></head><body onload="evil()"><h1 class="safe">Hello</h1><a href="next" ping="/ping" download>Next</a><img src="pic.png" srcset="/leak 2x" onerror="evil()"><form action="/submit"><input name="q"></form><iframe src="/frame"></iframe><svg><image href="/leak"></svg><script>evil()</script><p style="color:green;behavior:url(/evil);margin:2px">Static text</p></body></html>`, "text/html"},
		"https://example.com/assets/main.css": {`@import "more.css"; @font-face{font-family:test;src:url(font.woff)} h1{font-size:24px;background-image:url(pic.png)} @media screen { p { display:block; } }`, "text/css"},
		"https://example.com/assets/more.css": {`@import "main.css"; body{background-color:white}`, "text/css"},
		"https://example.com/assets/pic.png":  {tinyPNG(t), "image/png"},
	}
	m, calls := snapshotMod(t, fixtures)
	p, err := m.snapshot(context.Background(), "https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{snapshotCSP, "color:red", "padding:4px", "font-size:24px", "background-color:white", "color:blue", "data:image/png;base64,", "margin:2px"} {
		if !strings.Contains(html.UnescapeString(p.HTML), want) {
			t.Errorf("missing %q: %s", want, p.HTML)
		}
	}
	for _, bad := range []string{"onload", "onerror", "<script", "<iframe", "<form", "<svg", "srcset", "href=", "ping=", "evil()", "var(", "@font-face", "u\\72l"} {
		if strings.Contains(p.HTML, bad) {
			t.Errorf("unsafe %q: %s", bad, p.HTML)
		}
	}
	if len(*calls) != 4 || p.Dropped < 4 || p.Missing != 0 || len(p.Links) != 1 || p.Links[0].URL != "https://example.com/assets/next" {
		t.Fatalf("result %+v; calls %v", p, *calls)
	}
	if p.Title != "Fixture" || !strings.Contains(p.Text, "Static text") {
		t.Fatalf("metadata %+v", p)
	}
}

func TestSnapshotCSSDeclarationRecovery(t *testing.T) {
	b := &snapshotBuilder{ctx: context.Background(), images: map[string]string{}, imports: map[string]bool{}}
	base, _ := url.Parse("https://example.com/")
	cases := []string{
		`p { color:red; bad:123; padding:2px; background:image-set("https://example.org/a" 1x); margin:3px }`,
		`p { color:red; background:u\72l(https://example.org); padding:2px; margin:3px }`,
		`p { color:red; --x: url(https://example.org); padding:2px; margin:3px }`,
		`p { color:red; broken ???; padding:2px; margin:3px }`,
		`@font-face{src:url(https://example.org)} p{color:red;padding:2px;margin:3px}`,
	}
	for _, raw := range cases {
		got := b.css(raw, base, false, 0)
		for _, want := range []string{"color:red", "padding:2px", "margin:3px"} {
			if !strings.Contains(got, want) {
				t.Errorf("lost %s from %s => %s", want, raw, got)
			}
		}
		if strings.Contains(got, "example.org") {
			t.Error(got)
		}
	}
}

func TestSnapshotMissingAssetsAndDOMLimits(t *testing.T) {
	m, _ := snapshotMod(t, map[string]snapshotFixture{"https://example.com/": {`<img src="data:image/svg+xml,bad"><img src="/missing"><p style="color:red">Kept</p>`, "text/html"}})
	p, err := m.snapshot(context.Background(), "https://example.com/")
	if err != nil || p.Missing != 2 || !strings.Contains(p.HTML, "Kept") {
		t.Fatalf("%+v %v", p, err)
	}
	for _, raw := range []string{strings.Repeat("<div>", maxSnapshotDepth+1), strings.Repeat("<br>", maxSnapshotNodes+1)} {
		if _, err := snapshotDOM(context.Background(), []byte(raw)); err == nil {
			t.Fatal("complex HTML accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.snapshot(ctx, "https://example.com/"); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestSnapshotImagePixelBomb(t *testing.T) {
	raw := []byte(tinyPNG(t))
	binary.BigEndian.PutUint32(raw[16:20], 100000)
	binary.BigEndian.PutUint32(raw[20:24], 100000)
	binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))
	m, _ := snapshotMod(t, map[string]snapshotFixture{"https://example.com/": {`<img src="/huge.png">`, "text/html"}, "https://example.com/huge.png": {string(raw), "image/png"}})
	p, err := m.snapshot(context.Background(), "https://example.com/")
	if err != nil || p.Missing != 1 || strings.Contains(p.HTML, "data:image") {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestSnapshotResourcesUsePublicTransport(t *testing.T) {
	// The same pinned transport used by agents must handle every asset too.
	tr, calls := fixtureTransport(t, func(r *http.Request) string {
		ct, body := "text/html", `<link rel="stylesheet" href="/style.css"><img src="http://localhost/leak">`
		if r.URL.Path == "/style.css" {
			ct, body = "text/css", `@import "http://localhost/leak"; p{background:url(http://localhost/image);color:red}`
		}
		return fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: %s\r\nContent-Length: %d\r\n\r\n%s", ct, len(body), body)
	})
	m := &mod{public: newGuardedFetcher(), snapshots: make(chan struct{}, maxSnapshotConcurrency)}
	m.public.client.Transport = tr
	p, err := m.snapshot(context.Background(), "http://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || p.Missing != 3 {
		t.Fatalf("calls %d result %+v", calls.Load(), p)
	}
}

func TestSnapshotSlotCancellationAndOutputBound(t *testing.T) {
	m, _ := snapshotMod(t, nil)
	for i := 0; i < maxSnapshotConcurrency; i++ {
		m.snapshots <- struct{}{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := m.snapshot(ctx, "https://example.com/"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	b := boundedBuffer{limit: 4}
	if _, err := b.WriteString("1234"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("5")); err == nil || b.Len() != 4 {
		t.Fatal("unbounded output")
	}
}

func TestSnapshotRouteAndAgentMetadata(t *testing.T) {
	m, _ := snapshotMod(t, map[string]snapshotFixture{"https://example.com/": {`<h1>Words</h1><a href="/next">Next</a>`, "text/html"}})
	h := moduletest.Start(t, m)
	limitsResponse := httptest.NewRecorder()
	h.ServeHTTP(limitsResponse, httptest.NewRequest(http.MethodGet, "/api/m/browser/snapshot-limits", nil))
	var limits map[string]int
	if err := json.Unmarshal(limitsResponse.Body.Bytes(), &limits); err != nil || limits["max_response_bytes"] != maxSerializedBytes || limits["max_windows"] != maxSnapshotConcurrency {
		t.Fatalf("limits %+v %v", limits, err)
	}
	for _, actor := range []module.Actor{module.ActorOwner, module.ActorAgent} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/m/browser/snapshot?url=https://example.com/", nil)
		req = req.WithContext(module.WithActor(req.Context(), actor))
		h.ServeHTTP(rr, req)
		if rr.Code != 200 || rr.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%d %s", rr.Code, rr.Body.String())
		}
		var p Snapshot
		if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil || !strings.Contains(p.HTML, "Words") {
			t.Fatalf("%+v %v", p, err)
		}
		rr = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/m/browser/snapshot?url=http://localhost/", nil)
		req = req.WithContext(module.WithActor(req.Context(), actor))
		h.ServeHTTP(rr, req)
		if rr.Code == 200 {
			t.Fatal("owner snapshot bypassed public policy")
		}
	}
	result, err := h.Tool("browser_snapshot", `{"url":"https://example.com/"}`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "<h1>") || strings.Contains(string(encoded), `"html"`) {
		t.Fatal("HTML exposed by text tool")
	}
	if !strings.Contains(string(encoded), "Words") {
		t.Fatal(string(encoded))
	}
}

func TestSnapshotRedirectedStylesAndResourceLimits(t *testing.T) {
	m, calls := snapshotMod(t, map[string]snapshotFixture{
		"https://example.com/":                     {`<link rel="stylesheet" href="/style.css">`, "text/html"},
		"https://example.com/style.css":            {`p { background:url(photo.png); color:red }`, "text/css"},
		"https://example.com/redirected/photo.png": {tinyPNG(t), "image/png"},
	})
	inner := m.public.client.Transport
	m.public.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		resp, err := inner.RoundTrip(r)
		if err == nil && r.URL.Path == "/style.css" {
			copyReq := r.Clone(r.Context())
			copyReq.URL, _ = url.Parse("https://example.com/redirected/style.css")
			resp.Request = copyReq
		}
		return resp, err
	})
	p, err := m.snapshot(context.Background(), "https://example.com/")
	if err != nil || p.Missing != 0 || !strings.Contains(p.HTML, "data:image/png") {
		t.Fatalf("%+v %v %v", p, err, *calls)
	}
	b := &snapshotBuilder{ctx: context.Background(), client: m.public.client, resources: maxResources}
	if _, _, err = b.fetch("https://example.com/", maxHTML, "text/html"); err == nil {
		t.Fatal("resource limit ignored")
	}
	b.resources = 0
	b.bytes = maxSnapshotBytes
	if _, _, err = b.fetch("https://example.com/", maxHTML, "text/html"); err == nil {
		t.Fatal("total bytes ignored")
	}
	b.bytes = maxSnapshotBytes - 1
	if _, _, err = b.fetch("https://example.com/", maxHTML, "text/html"); err == nil || b.bytes != maxSnapshotBytes {
		t.Fatal("failed response budget refunded")
	}
}

func TestSnapshotReservationHeldThroughConsumer(t *testing.T) {
	m, _ := snapshotMod(t, map[string]snapshotFixture{"https://example.com/": {`<p>Test</p>`, "text/html"}})
	expected := errors.New("consumer failed")
	err := m.withSnapshot(context.Background(), "https://example.com/", func(Snapshot) error {
		if len(m.snapshots) != 1 {
			t.Error("reservation released before response delivery")
		}
		return expected
	})
	if !errors.Is(err, expected) || len(m.snapshots) != 0 {
		t.Fatalf("reservation leak/error: %v %d", err, len(m.snapshots))
	}
}

func TestSnapshotAggregateRasterBudget(t *testing.T) {
	m, _ := snapshotMod(t, map[string]snapshotFixture{"https://example.com/p.png": {tinyPNG(t), "image/png"}})
	b := &snapshotBuilder{ctx: context.Background(), client: m.public.client, images: map[string]string{}, pixels: maxSnapshotPixels}
	base, _ := url.Parse("https://example.com/")
	if got := b.raster(base, "p.png"); got != "" || b.result.Missing != 1 {
		t.Fatal("aggregate pixel budget ignored")
	}
}

func TestSnapshotRequiresURL(t *testing.T) {
	m, _ := snapshotMod(t, nil)
	h := moduletest.Start(t, m)
	for _, query := range []string{"", "?url=", "?url=%20%20"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/m/browser/snapshot"+query, nil))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "url is required") {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body.String())
		}
	}
	for _, input := range []string{`{}`, `{"url":""}`, `{"url":"  "}`} {
		if _, err := h.Tool("browser_snapshot", input); err == nil || !strings.Contains(err.Error(), "url is required") {
			t.Fatalf("%s: %v", input, err)
		}
	}
}
