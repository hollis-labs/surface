package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module/tetherlink"
)

// FleetDisk exposes only health facts. Connection details, inventory serials and credentials
// from Cerberus never cross this projection into the page or agent context.
type FleetDisk struct {
	Mount string   `json:"mount"`
	Used  *float64 `json:"used"`
	Total *float64 `json:"total"`
}
type FleetMetrics struct {
	CPUPercent      *float64    `json:"cpu_percent,omitempty"`
	MemoryUsed      *float64    `json:"memory_used,omitempty"`
	Disks           []FleetDisk `json:"disks,omitempty"`
	CPU             *float64    `json:"cpu,omitempty"`
	Load1           *float64    `json:"load1,omitempty"`
	Load5           *float64    `json:"load5,omitempty"`
	Load15          *float64    `json:"load15,omitempty"`
	MemoryAvailable *float64    `json:"memory_available,omitempty"`
	MemoryTotal     *float64    `json:"memory_total,omitempty"`
	DiskFree        *float64    `json:"disk_free,omitempty"`
	DiskTotal       *float64    `json:"disk_total,omitempty"`
}
type FleetHost struct {
	Name     string       `json:"name"`
	Role     string       `json:"role"`
	State    string       `json:"state"`
	LastSeen string       `json:"last_seen,omitempty"`
	ProbedAt string       `json:"probed_at,omitempty"`
	Stale    bool         `json:"stale"`
	Metrics  FleetMetrics `json:"metrics"`
}
type FleetSample struct {
	SampleAt string `json:"sample_at,omitempty"`
	At       string `json:"at"`
	Count    int    `json:"count"`
	FleetMetrics
}
type FleetResult struct {
	Source    string `json:"source"`
	history   map[string][]FleetSample
	Hosts     []FleetHost   `json:"hosts"`
	Host      string        `json:"host,omitempty"`
	Samples   []FleetSample `json:"samples"`
	FetchedAt string        `json:"fetched_at"`
	Truncated bool          `json:"truncated"`
	Warning   string        `json:"warning,omitempty"`
}
type fleetRead struct {
	result FleetResult
	err    error
	at     time.Time
}
type fleetCache struct {
	mu      sync.Mutex
	hosts   fleetRead
	samples map[string]fleetRead
}

const fleetTTL = 30 * time.Second

// Reads are serialized and cached, including failures, so multiple windows and tools
// cannot multiply upstream traffic. Only the selected host's history is requested.
func (c *fleetCache) read(ctx context.Context, link tetherlink.Caller, host string) (FleetResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.hosts.at) >= fleetTTL {
		result, err := readFleetSnapshot(ctx, link)
		if err != nil {
			result, err = readFleetHosts(ctx, link)
			if err == nil {
				result.Warning = "Fleet snapshot unavailable; showing inventory and averaged samples. CPU utilization and per-mount disks may be unavailable."
			}
		}
		c.hosts = fleetRead{result, err, time.Now()}
	}
	if c.hosts.err != nil {
		return FleetResult{}, c.hosts.err
	}
	result := c.hosts.result
	result.Host = host
	result.Samples = []FleetSample{}
	if host == "" {
		return result, nil
	}
	found := false
	for _, h := range result.Hosts {
		if h.Name == host {
			found = true
			break
		}
	}
	if !found {
		return result, fmt.Errorf("fleet host %q is not in the inventory", host)
	}
	if result.Source == "snapshot" {
		result.Samples = result.history[host]
		return result, nil
	}
	if c.samples == nil {
		c.samples = map[string]fleetRead{}
	}
	cached, ok := c.samples[host]
	if !ok || time.Since(cached.at) >= fleetTTL {
		samples, err := readFleetSamples(ctx, link, host)
		cached = fleetRead{result: FleetResult{Samples: samples}, err: err, at: time.Now()}
		c.samples[host] = cached
		// Entries are bounded by the bounded host inventory.
		if len(c.samples) > 100 {
			clear(c.samples)
			c.samples[host] = cached
		}
	}
	if cached.err != nil {
		result.Warning += " Recent health samples unavailable."
	} else {
		result.Samples = cached.result.Samples
	}
	return result, nil
}

func readFleetHosts(ctx context.Context, link tetherlink.Caller) (FleetResult, error) {
	result := FleetResult{Source: "inventory", Hosts: []FleetHost{}, Samples: []FleetSample{}, FetchedAt: time.Now().UTC().Format(time.RFC3339)}
	seen := map[string]bool{}
	for page := 0; page < 4; page++ {
		raw, err := tetherlink.CallAll(ctx, link, "cerberus_host_list", map[string]any{"limit": 25, "offset": page * 25})
		if err != nil {
			return result, fmt.Errorf("fleet inventory unavailable: %w", err)
		}
		var data struct {
			Items []struct {
				Host struct {
					Name string `json:"name"`
					Role string `json:"role"`
				} `json:"host"`
				State  string `json:"state"`
				Status struct {
					Status string `json:"status"`
				} `json:"status"`
				LastSeen string       `json:"last_seen"`
				ProbedAt string       `json:"probed_at"`
				Stale    bool         `json:"stale"`
				Probed   FleetMetrics `json:"probed"`
			} `json:"items"`
		}
		if err = json.Unmarshal([]byte(raw), &data); err != nil || data.Items == nil {
			return result, fmt.Errorf("fleet inventory returned an unreadable result")
		}
		for _, item := range data.Items {
			if item.Host.Name == "" || seen[item.Host.Name] {
				continue
			}
			seen[item.Host.Name] = true
			state := item.Status.Status
			if state == "" {
				state = item.State
			}
			if state == "" {
				state = "unknown"
			}
			result.Hosts = append(result.Hosts, FleetHost{Name: item.Host.Name, Role: item.Host.Role, State: state, LastSeen: item.LastSeen, ProbedAt: item.ProbedAt, Stale: item.Stale, Metrics: item.Probed})
		}
		if len(data.Items) < 25 {
			break
		}
		if page == 3 {
			result.Truncated = true
		}
	}
	sort.Slice(result.Hosts, func(i, j int) bool { return result.Hosts[i].Name < result.Hosts[j].Name })
	return result, nil
}
func readFleetSamples(ctx context.Context, link tetherlink.Caller, host string) ([]FleetSample, error) {
	raw, err := tetherlink.CallAll(ctx, link, "cerberus_host_samples", map[string]any{"name": host, "grain": "5m", "since": "1h", "limit": 12})
	if err != nil {
		return nil, err
	}
	type average struct {
		Avg *float64 `json:"avg"`
	}
	var rows []struct {
		At          string   `json:"ts"`
		Count       int      `json:"n"`
		CPU         *float64 `json:"cpu"`
		MemoryTotal *float64 `json:"memory_total"`
		DiskTotal   *float64 `json:"disk_total"`
		Load        average  `json:"load1"`
		Memory      average  `json:"memory_available"`
		Disk        average  `json:"disk_free"`
	}
	if err = json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, err
	}
	out := make([]FleetSample, 0, len(rows))
	for _, r := range rows {
		out = append(out, FleetSample{At: r.At, Count: r.Count, FleetMetrics: FleetMetrics{CPU: r.CPU, Load1: r.Load.Avg, MemoryAvailable: r.Memory.Avg, MemoryTotal: r.MemoryTotal, DiskFree: r.Disk.Avg, DiskTotal: r.DiskTotal}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At < out[j].At })
	if len(out) > 12 {
		out = out[len(out)-12:]
	}
	return out, nil
}

// fleet joins observations only on the upstream host name; unknown placement stays
// visible as a count instead of being guessed from an agent or session identifier.
func (m *mod) fleet(ctx context.Context, host string) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	link, err := m.tether()
	if err != nil {
		return nil, err
	}
	result, err := m.liveFleet.read(ctx, link, host)
	if err != nil {
		return nil, err
	}
	sessions, sessionErr := m.sessions(ctx)
	selected := []SessionRow{}
	unknown := 0
	for _, s := range sessions.Sessions {
		switch s.Host {
		case "":
			unknown++
		case host:
			selected = append(selected, s)
		}
	}
	warning := sessions.Warning
	if sessionErr != nil {
		warning = "Agent observations unavailable"
	}
	return struct {
		FleetResult
		Sessions            []SessionRow `json:"sessions"`
		SessionsWarning     string       `json:"sessions_warning,omitempty"`
		SessionsTruncated   bool         `json:"sessions_truncated"`
		UnknownHostSessions int          `json:"unknown_host_sessions"`
	}{result, selected, warning, sessions.Truncated, unknown}, nil
}

// The versioned snapshot is one read of already collected facts, not a probe fan-out.
func readFleetSnapshot(ctx context.Context, link tetherlink.Caller) (FleetResult, error) {
	raw, err := tetherlink.CallAll(ctx, link, "cerberus_fleet_snapshot", map[string]any{"limit": 100, "offset": 0})
	if err != nil {
		return FleetResult{}, err
	}
	type point struct {
		TS       string `json:"ts"`
		SampleAt string `json:"sample_at"`
		FleetMetrics
	}
	var data struct {
		Schema      string `json:"schema"`
		GeneratedAt string `json:"generated_at"`
		NextOffset  *int   `json:"next_offset"`
		Hosts       []struct {
			Name      string   `json:"name"`
			Roles     []string `json:"roles"`
			Reachable *bool    `json:"reachable"`
			LastSeen  string   `json:"last_seen"`
			Stale     bool     `json:"stale"`
			Latest    *point   `json:"latest"`
			Series    []point  `json:"series"`
		} `json:"hosts"`
	}
	if json.Unmarshal([]byte(raw), &data) != nil || data.Schema != "cerberus.fleet.snapshot.v1" || data.Hosts == nil {
		return FleetResult{}, fmt.Errorf("unreadable fleet snapshot")
	}
	out := FleetResult{Source: "snapshot", Hosts: []FleetHost{}, Samples: []FleetSample{}, FetchedAt: data.GeneratedAt, Truncated: data.NextOffset != nil, history: map[string][]FleetSample{}}
	for i, h := range data.Hosts {
		if i >= 100 {
			out.Truncated = true
			break
		}
		row := FleetHost{Name: h.Name, Role: strings.Join(h.Roles, ", "), State: "unknown", LastSeen: h.LastSeen, Stale: h.Stale}
		if h.Reachable != nil {
			if *h.Reachable {
				row.State = "online"
			} else {
				row.State = "offline"
			}
		}
		if h.Latest != nil {
			row.Metrics = h.Latest.FleetMetrics
			row.ProbedAt = h.Latest.TS
		}
		out.Hosts = append(out.Hosts, row)
		samples := []FleetSample{}
		for j, p := range h.Series {
			if j >= 12 {
				break
			}
			sample := FleetSample{At: p.TS, SampleAt: p.SampleAt}
			if p.SampleAt != "" {
				sample.Count = 1
				sample.FleetMetrics = p.FleetMetrics
			}
			samples = append(samples, sample)
		}
		out.history[h.Name] = samples
	}
	return out, nil
}
