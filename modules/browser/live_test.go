package browser

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveFetchAndSearch(t *testing.T) {
	if os.Getenv("SURFACE_LIVE") == "" {
		t.Skip("SURFACE_LIVE not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := newFetcher()
	p, err := f.get(ctx, "https://go.dev/doc/")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("title %q framable %v text %d chars links %d", p.Title, p.Framable, len(p.Text), len(p.Links))
	r, err := f.search(ctx, "golang net/http ServeMux patterns")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d results; first %+v", len(r), r[0])
}
