package ops

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/hollis-labs/surface/module"
	"strings"
	"sync"
	"testing"
	"time"
)

type fleetFake struct {
	mu    sync.Mutex
	calls map[string]int
	fail  bool
}

func (f *fleetFake) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[tool]++
	if f.fail {
		return "", errors.New("offline")
	}
	switch tool {
	case "cerberus_host_list":
		return `{"items":[{"host":{"name":"test-host","role":"worker","reach":{"token_file":"secret-path"}},"state":"online","status":{"status":"degraded"},"stale":true,"probed":{"cpu":8,"load1":0,"memory_available":0,"memory_total":100,"disk_free":20,"disk_total":100}}]}`, nil
	case "cerberus_host_samples":
		if args["name"] != "test-host" || args["grain"] != "5m" || args["limit"] != 12 || args["since"] != "1h" {
			return "", errors.New("unbounded sample request")
		}
		return `[{"ts":"2026-01-01T12:05:00Z","n":2,"cpu":8,"load1":{"avg":2},"memory_available":{"avg":50},"memory_total":100,"disk_free":{"avg":10},"disk_total":100},{"ts":"2026-01-01T12:00:00Z","n":0}]`, nil
	}
	return "", errors.New("unexpected tool " + tool)
}
func TestFleetProjectionAndBoundedSharedCache(t *testing.T) {
	f := &fleetFake{}
	c := &fleetCache{}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			r, err := c.read(t.Context(), f, "test-host")
			if err != nil {
				t.Error(err)
				return
			}
			if len(r.Hosts) != 1 || r.Hosts[0].State != "degraded" || !r.Hosts[0].Stale {
				t.Errorf("host health: %+v", r)
			}
			if r.Hosts[0].Metrics.Load1 == nil || *r.Hosts[0].Metrics.Load1 != 0 {
				t.Error("zero load lost")
			}
			if len(r.Samples) != 2 || r.Samples[0].Count != 0 || r.Samples[0].Load1 != nil || *r.Samples[1].Load1 != 2 {
				t.Errorf("samples: %+v", r.Samples)
			}
		})
	}
	wg.Wait()
	if f.calls["cerberus_host_list"] != 1 || f.calls["cerberus_host_samples"] != 1 {
		t.Fatalf("calls multiplied: %v", f.calls)
	}
	if _, err := c.read(t.Context(), f, "unknown-host"); err == nil {
		t.Fatal("unknown host accepted")
	}
	if f.calls["cerberus_host_samples"] != 1 {
		t.Fatal("unknown host fetched")
	}
}
func TestFleetCachesUpstreamFailure(t *testing.T) {
	f := &fleetFake{fail: true}
	c := &fleetCache{}
	for range 3 {
		if _, err := c.read(t.Context(), f, ""); err == nil || !strings.Contains(err.Error(), "unavailable") {
			t.Fatal(err)
		}
	}
	if f.calls["cerberus_host_list"] != 1 {
		t.Fatal("failure hammered upstream")
	}
}

type snapshotFake struct {
	calls int
	raw   string
}

func (f *snapshotFake) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	if tool != "cerberus_fleet_snapshot" {
		return "", errors.New("unexpected fallback")
	}
	f.calls++
	if args["limit"] != 100 || args["offset"] != 0 {
		return "", errors.New("unbounded snapshot")
	}
	return f.raw, nil
}
func TestFleetSnapshotRetainsUnknownAndPerMountObservations(t *testing.T) {
	f := &snapshotFake{raw: `{"schema":"cerberus.fleet.snapshot.v1","generated_at":"2026-01-01T12:00:00Z","next_offset":100,"hosts":[{"name":"test-host","roles":["worker"],"reachable":null,"stale":true,"latest":{"ts":"2026-01-01T11:59:00Z","cpu_percent":0,"memory_used":20,"memory_total":100,"disks":[{"mount":"/var/tmp","used":10,"total":100}],"load1":0},"series":[{"ts":"2026-01-01T11:50:00Z","sample_at":null,"cpu_percent":99},{"ts":"2026-01-01T11:55:00Z","sample_at":"2026-01-01T11:59:00Z","cpu_percent":0,"disks":[{"mount":"/var/tmp","used":10,"total":100}]}]}]}`}
	c := &fleetCache{}
	r, err := c.read(t.Context(), f, "test-host")
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "snapshot" || !r.Truncated || r.Hosts[0].State != "unknown" {
		t.Fatalf("snapshot %+v", r)
	}
	if r.Hosts[0].Metrics.CPUPercent == nil || *r.Hosts[0].Metrics.CPUPercent != 0 {
		t.Fatal("zero utilization lost")
	}
	if len(r.Samples) != 2 || r.Samples[0].CPUPercent != nil || r.Samples[1].SampleAt == "" || r.Samples[1].Disks[0].Mount != "/var/tmp" {
		t.Fatalf("sample gaps corrupted: %+v", r.Samples)
	}
	if _, err = c.read(t.Context(), f, ""); err != nil || f.calls != 1 {
		t.Fatalf("cache: %v %d", err, f.calls)
	}
}

func TestFleetToolJoinsOnlyAuthoritativeHostPlacement(t *testing.T) {
	m := &mod{caller: &fleetFake{}}
	m.liveSessions = sessionCache{until: time.Now().Add(time.Minute), result: SessionsResult{Sessions: []SessionRow{
		{ID: "on-host", Host: "test-host"}, {ID: "elsewhere", Host: "other"}, {ID: "test-host-guessed", Host: ""},
	}}}
	for _, tool := range m.tools() {
		if tool.Name != "ops_fleet" {
			continue
		}
		if tool.Level != module.LevelRead {
			t.Fatal("fleet is not read-only")
		}
		result, err := tool.Run(t.Context(), nil, json.RawMessage(`{"host":"test-host"}`))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "secret-path") {
			t.Fatal("private inventory escaped projection")
		}
		var got struct {
			Sessions []SessionRow `json:"sessions"`
			Unknown  int          `json:"unknown_host_sessions"`
		}
		if err = json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Sessions) != 1 || got.Sessions[0].ID != "on-host" || got.Unknown != 1 {
			t.Fatalf("placement: %+v", got)
		}
		return
	}
	t.Fatal("missing fleet tool")
}
