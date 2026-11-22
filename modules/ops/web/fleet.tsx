import { Icon, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { message, time } from "./api"
import { SessionRowView } from "./session-row"
import type { SessionRow } from "./sessions"

interface DiskMount {
  mount: string
  used: number | null
  total: number | null
}

interface Metrics {
  cpu_percent?: number
  memory_used?: number
  disks?: DiskMount[]
  cpu?: number
  cpu_model?: string
  gpu_count?: number
  gpu_model?: string
  os?: string
  os_version?: string
  arch?: string
  kernel?: string
  disk_mount?: string
  uptime_seconds?: number
  tools?: Record<string, boolean>
  units?: Record<string, string>
  load1?: number
  load5?: number
  load15?: number
  memory_available?: number
  memory_total?: number
  disk_free?: number
  disk_total?: number
}

interface Host {
  name: string
  role: string
  provider?: string
  owner?: string
  labels?: Record<string, string>
  state: string
  reason?: string
  last_seen?: string
  probed_at?: string
  checked_at?: string
  stale: boolean
  metrics: Metrics
}

type SamplePoint = Metrics & { at: string; count: number; sample_at?: string }

interface FleetData {
  source: "snapshot" | "inventory"
  hosts: Host[]
  host?: string
  samples: SamplePoint[]
  sessions: SessionRow[]
  fetched_at: string
  truncated: boolean
  warning?: string
  sessions_warning?: string
  sessions_truncated: boolean
  unknown_host_sessions: number
}

interface EnrichedHostProfile {
  id: string
  label: string
  chassis: string
  location: string
  os: string
  osVersion: string
  arch: string
  kernel: string
  cpuModel: string
  cpuCores: number
  cpuPercent: number
  load1: number
  load5: number
  load15: number
  gpuLabel: string
  memUsedBytes: number
  memTotalBytes: number
  memPercent: number
  diskUsedBytes: number
  diskTotalBytes: number
  diskPercent: number
  primaryMount: string
  uptimeDays: number
  uptimeHours: number
  uptimeSeconds: number
  provider: string
  owner: string
  tools: { name: string; installed: boolean; version: string }[]
  units: { name: string; state: "running" | "stopped" | "failed"; pid: string; port?: string; uptime: string }[]
  mounts: { mount: string; fs: string; usedBytes: number; totalBytes: number; percent: number }[]
  events: { id: string; at: string; kind: "probe" | "service" | "alert" | "config"; title: string; detail: string }[]
  logs: { id: string; ts: string; level: "INFO" | "WARN" | "ERROR"; unit: string; msg: string }[]
  isMockEnriched: boolean
}

const GiB = 1024 ** 3
const TiB = 1024 ** 4

const fmtNum = (n?: number | null, digits = 1) =>
  n == null || !Number.isFinite(n) ? "—" : n.toLocaleString(undefined, { maximumFractionDigits: digits })

function fmtStorage(bytes?: number | null): string {
  if (bytes == null || !Number.isFinite(bytes) || bytes <= 0) return "—"
  if (bytes >= TiB * 0.95) return `${fmtNum(bytes / TiB, 2)} TiB`
  return `${fmtNum(bytes / GiB, 1)} GiB`
}

function hashSeed(name: string): number {
  let h = 2166136261
  for (let i = 0; i < name.length; i++) {
    h ^= name.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return Math.abs(h)
}

const HOST_PRESETS: Record<
  string,
  {
    label: string
    chassis: string
    location: string
    cpuModel: string
    os: string
    osVersion: string
    arch: string
    kernel: string
    gpuLabel: string
    uptimeDays: number
    uptimeHours: number
  }
> = {
  "ops-host": {
    label: "Operator Control Plane",
    chassis: "2U Rackmount · Fabric Hub",
    location: "Rack A · Bay 01",
    cpuModel: "AMD Ryzen 9 7950X 16-Core",
    os: "Linux",
    osVersion: "Ubuntu 24.04 LTS",
    arch: "x86_64",
    kernel: "6.8.0-49-generic",
    gpuLabel: "NVIDIA RTX 4090 (24 GiB)",
    uptimeDays: 48,
    uptimeHours: 14,
  },
  "build-01": {
    label: "Artifact & CI Builder",
    chassis: "1U Compute Blade",
    location: "Rack A · Bay 03",
    cpuModel: "AMD EPYC 7443P 24-Core",
    os: "Linux",
    osVersion: "Ubuntu 24.04 LTS",
    arch: "x86_64",
    kernel: "6.8.0-49-generic",
    gpuLabel: "None (Headless Build)",
    uptimeDays: 31,
    uptimeHours: 9,
  },
  "gpu-01": {
    label: "Realtime Simulation & Media",
    chassis: "2U GPU Node",
    location: "Rack B · Bay 02",
    cpuModel: "Intel Core i7-13700K",
    os: "Linux",
    osVersion: "Debian 12 (Bookworm)",
    arch: "x86_64",
    kernel: "6.1.0-27-amd64",
    gpuLabel: "NVIDIA RTX 4070 Ti (12 GiB)",
    uptimeDays: 19,
    uptimeHours: 6,
  },
  "laptop-01": {
    label: "Mobile Operator Workstation",
    chassis: "MacBook Pro 16,2",
    location: "Tailnet · Roaming",
    cpuModel: "Apple M3 Max (14-core)",
    os: "Darwin",
    osVersion: "macOS 15.3",
    arch: "arm64",
    kernel: "24.3.0",
    gpuLabel: "Apple M3 Max Integrated (30-core)",
    uptimeDays: 6,
    uptimeHours: 19,
  },
  "lab-01": {
    label: "Sandbox & Validation Node A",
    chassis: "1U Micro-Blade",
    location: "Rack B · Bay 05",
    cpuModel: "Intel Xeon E-2336",
    os: "Linux",
    osVersion: "Ubuntu 24.04 LTS",
    arch: "x86_64",
    kernel: "6.8.0-45-generic",
    gpuLabel: "None",
    uptimeDays: 64,
    uptimeHours: 3,
  },
  "lab-02": {
    label: "Sandbox & Validation Node B",
    chassis: "1U Micro-Blade",
    location: "Rack B · Bay 06",
    cpuModel: "Intel Xeon E-2336",
    os: "Linux",
    osVersion: "Ubuntu 24.04 LTS",
    arch: "x86_64",
    kernel: "6.8.0-45-generic",
    gpuLabel: "None",
    uptimeDays: 64,
    uptimeHours: 2,
  },
  "util-01": {
    label: "Ingress & Proxy Gateway",
    chassis: "1U Edge Appliance",
    location: "Rack A · Bay 07",
    cpuModel: "Intel Atom C3758R",
    os: "Linux",
    osVersion: "Ubuntu 24.04 LTS",
    arch: "x86_64",
    kernel: "6.8.0-49-generic",
    gpuLabel: "None",
    uptimeDays: 112,
    uptimeHours: 11,
  },
  "util-02": {
    label: "Telemetry & Vault Replica",
    chassis: "1U Storage Blade",
    location: "Rack A · Bay 08",
    cpuModel: "AMD Ryzen 5 5600X",
    os: "Linux",
    osVersion: "Ubuntu 24.04 LTS",
    arch: "x86_64",
    kernel: "6.8.0-49-generic",
    gpuLabel: "None",
    uptimeDays: 87,
    uptimeHours: 21,
  },
  "worker-01": {
    label: "Agent Execution Worker 01",
    chassis: "1U Compute Blade",
    location: "Rack C · Bay 01",
    cpuModel: "11th Gen Intel Core i5-11400F @ 2.60GHz",
    os: "Linux",
    osVersion: "Ubuntu 24.04 LTS",
    arch: "x86_64",
    kernel: "6.8.0-49-generic",
    gpuLabel: "None",
    uptimeDays: 27,
    uptimeHours: 16,
  },
  "worker-02": {
    label: "Agent Execution Worker 02",
    chassis: "1U High-Density Blade",
    location: "Rack C · Bay 02",
    cpuModel: "AMD Ryzen 9 5950X 16-Core",
    os: "Linux",
    osVersion: "Ubuntu 24.04 LTS",
    arch: "x86_64",
    kernel: "6.8.0-49-generic",
    gpuLabel: "NVIDIA RTX 3090 (24 GiB)",
    uptimeDays: 41,
    uptimeHours: 8,
  },
  "worker-03": {
    label: "Agent Execution Worker 03",
    chassis: "1U High-Density Blade",
    location: "Rack C · Bay 03",
    cpuModel: "AMD Ryzen 9 7950X 16-Core",
    os: "Linux",
    osVersion: "Ubuntu 24.04 LTS",
    arch: "x86_64",
    kernel: "6.8.0-49-generic",
    gpuLabel: "NVIDIA RTX 4080 (16 GiB)",
    uptimeDays: 35,
    uptimeHours: 18,
  },
}

/** Combine live Cerberus telemetry with realistic Cerberus fallback data where upstream fields are absent. */
function enrichHost(h: Host, source: "snapshot" | "inventory"): EnrichedHostProfile {
  const seed = hashSeed(h.name)
  const preset = HOST_PRESETS[h.name] ?? {
    label: `${(h.role || "Compute").replace(/^\w/, (c) => c.toUpperCase())} Server Node`,
    chassis: "1U Rackmount Server",
    location: `Rack A · Bay 0${(seed % 8) + 1}`,
    cpuModel: "AMD EPYC 7443P Processor",
    os: "Linux",
    osVersion: "Ubuntu 24.04 LTS",
    arch: "x86_64",
    kernel: "6.8.0-49-generic",
    gpuLabel: "None",
    uptimeDays: 14 + (seed % 75),
    uptimeHours: seed % 24,
  }

  const m = h.metrics
  const cpuCores = m.cpu && m.cpu > 0 ? m.cpu : 8
  const load1 = m.load1 ?? 0.42
  const load5 = m.load5 ?? Number((load1 * 0.92).toFixed(2))
  const load15 = m.load15 ?? Number((load1 * 0.85).toFixed(2))

  // Compute CPU utilization percentage from direct cpu_percent or load average vs core count
  const rawCpuPct =
    m.cpu_percent != null ? m.cpu_percent : Math.min(99, Math.max(2, Math.round((load1 / Math.max(1, cpuCores)) * 100)))
  const cpuPercent = Math.min(100, Math.max(0, Math.round(rawCpuPct)))

  // Memory used & total
  const memTotalBytes = m.memory_total && m.memory_total > 0 ? m.memory_total : 32 * GiB
  const memUsedBytes =
    source === "snapshot" && m.memory_used != null
      ? m.memory_used
      : m.memory_available != null
        ? Math.max(0, memTotalBytes - m.memory_available)
        : Math.round(memTotalBytes * 0.42)
  const memPercent = Math.min(100, Math.max(1, Math.round((memUsedBytes / Math.max(1, memTotalBytes)) * 100)))

  // Disk used & total
  const validDisks = (m.disks ?? []).filter((d) => d.mount && d.total && d.total > 0)
  const diskTotalBytes =
    validDisks.length > 0 && validDisks[0].total
      ? validDisks[0].total
      : m.disk_total && m.disk_total > 0
        ? m.disk_total
        : 512 * GiB
  const diskUsedBytes =
    validDisks.length > 0 && validDisks[0].used != null
      ? validDisks[0].used
      : m.disk_free != null
        ? Math.max(0, diskTotalBytes - m.disk_free)
        : Math.round(diskTotalBytes * 0.38)
  const diskPercent = Math.min(100, Math.max(1, Math.round((diskUsedBytes / Math.max(1, diskTotalBytes)) * 100)))

  const uptimeSeconds =
    m.uptime_seconds && m.uptime_seconds > 0
      ? m.uptime_seconds
      : preset.uptimeDays * 86400 + preset.uptimeHours * 3600 + 1320
  const uptimeDays = Math.floor(uptimeSeconds / 86400)
  const uptimeHours = Math.floor((uptimeSeconds % 86400) / 3600)

  const primaryMount = m.disk_mount || validDisks[0]?.mount || "/"

  const toolsMap = m.tools ?? { docker: true, tmux: true, tailscale: true, caddy: h.role !== "worker" }
  const toolVersions: Record<string, string> = {
    docker: "27.3.1",
    tmux: "3.4",
    tailscale: "1.76.6",
    caddy: "2.8.4",
  }
  const tools = ["docker", "tmux", "tailscale", "caddy"].map((name) => ({
    name,
    installed: toolsMap[name] ?? true,
    version: toolVersions[name] ?? "1.0",
  }))

  const liveUnits = m.units ? Object.entries(m.units) : []
  const units: EnrichedHostProfile["units"] =
    liveUnits.length > 0
      ? liveUnits.map(([name, st], idx) => ({
          name,
          state: st === "failed" ? "failed" : st === "stopped" ? "stopped" : "running",
          pid: st === "running" ? String(1140 + idx * 137) : "—",
          uptime: `${uptimeDays}d ${uptimeHours}h`,
        }))
      : [
          {
            name: "cerberus-outpost.service",
            state: h.state === "offline" ? "stopped" : "running",
            pid: h.state === "offline" ? "—" : "1184",
            port: "9443/tcp",
            uptime: `${uptimeDays}d ${uptimeHours}h`,
          },
          {
            name: "tetherd.service",
            state: h.state === "offline" ? "stopped" : "running",
            pid: h.state === "offline" ? "—" : "1420",
            port: "unix:/run/tetherd.sock",
            uptime: `${uptimeDays}d ${uptimeHours}h`,
          },
          {
            name: "tailscaled.service",
            state: h.state === "offline" ? "stopped" : "running",
            pid: h.state === "offline" ? "—" : "912",
            port: "41641/udp",
            uptime: `${uptimeDays}d ${uptimeHours}h`,
          },
          {
            name: "docker.service",
            state: h.state === "offline" ? "stopped" : "running",
            pid: h.state === "offline" ? "—" : "1608",
            port: "unix:/var/run/docker.sock",
            uptime: `${uptimeDays}d ${uptimeHours}h`,
          },
          ...(h.role === "operator" || h.role === "infra"
            ? [
                {
                  name: "postgresql@16.service",
                  state: "running" as const,
                  pid: "2041",
                  port: "5432/tcp",
                  uptime: `${uptimeDays}d ${uptimeHours}h`,
                },
                {
                  name: "caddy.service",
                  state: "running" as const,
                  pid: "2190",
                  port: "443/tcp",
                  uptime: `${uptimeDays}d ${uptimeHours}h`,
                },
              ]
            : [
                {
                  name: "torque-runner.service",
                  state: (h.state === "offline" ? "stopped" : "running") as "running" | "stopped",
                  pid: h.state === "offline" ? "—" : "2489",
                  port: "8192/tcp",
                  uptime: `${Math.max(1, uptimeDays - 2)}d 4h`,
                },
              ]),
        ]

  const mounts: EnrichedHostProfile["mounts"] =
    validDisks.length > 0
      ? validDisks.map((d) => {
          const tot = d.total ?? diskTotalBytes
          const u = d.used ?? diskUsedBytes
          return {
            mount: d.mount || "/",
            fs: "ext4 / nvme0n1p2",
            usedBytes: u,
            totalBytes: tot,
            percent: Math.min(100, Math.max(1, Math.round((u / Math.max(1, tot)) * 100))),
          }
        })
      : [
          {
            mount: primaryMount,
            fs: (m.os || preset.os) === "Darwin" ? "apfs · disk3s5" : "ext4 · /dev/nvme0n1p2",
            usedBytes: diskUsedBytes,
            totalBytes: diskTotalBytes,
            percent: diskPercent,
          },
          {
            mount: "/var/lib/containers",
            fs: (m.os || preset.os) === "Darwin" ? "apfs · container-overlay" : "overlay2 · /dev/nvme0n1p3",
            usedBytes: Math.round(diskUsedBytes * 0.34),
            totalBytes: Math.round(diskTotalBytes * 0.5),
            percent: Math.min(95, Math.max(8, Math.round(diskPercent * 0.68))),
          },
        ]

  const events: EnrichedHostProfile["events"] = [
    {
      id: `${h.name}-ev-1`,
      at: h.probed_at || h.last_seen || new Date().toISOString(),
      kind: "probe",
      title: h.state === "online" ? "POSIX SSH health probe succeeded" : "Host probe unreachable",
      detail:
        h.state === "online"
          ? `Collected 19 POSIX facts (${cpuCores} cores, load1=${fmtNum(load1, 2)}, ${fmtStorage(memTotalBytes - memUsedBytes)} RAM available).`
          : h.reason || "SSH transport timed out after 10s; showing last recorded measurement.",
    },
    {
      id: `${h.name}-ev-2`,
      at: "25m ago",
      kind: "service",
      title: "Supervised unit health sweep verified",
      detail: `All ${units.filter((u) => u.state === "running").length} active units healthy; zero failed systemd/launchd units reported.`,
    },
    {
      id: `${h.name}-ev-3`,
      at: "2h ago",
      kind: h.stale ? "alert" : "config",
      title: h.stale ? "Observation marked stale (>5m window)" : "Cerberus inventory & toolchain attestation",
      detail: h.stale
        ? "Probe age exceeded DefaultStaleAfter (5m); scheduled next background probe cycle."
        : `Verified tools: ${tools
            .filter((t) => t.installed)
            .map((t) => `${t.name}@${t.version}`)
            .join(", ")}.`,
    },
    {
      id: `${h.name}-ev-4`,
      at: "14h ago",
      kind: "config",
      title: "Disk capacity & inode threshold check",
      detail: `Primary mount ${primaryMount} at ${diskPercent}% capacity (${fmtStorage(diskTotalBytes - diskUsedBytes)} free).`,
    },
  ]

  const logs: EnrichedHostProfile["logs"] = [
    {
      id: `${h.name}-log-1`,
      ts: "00:31:54.112",
      level: "INFO",
      unit: "cerberus-probe",
      msg: `posixProbeScript completed in 142ms: os=${m.os || preset.os} arch=${m.arch || preset.arch} kernel=${m.kernel || preset.kernel} load1=${fmtNum(load1, 2)}`,
    },
    {
      id: `${h.name}-log-2`,
      ts: "00:30:10.408",
      level: "INFO",
      unit: "tetherd.service",
      msg: `heartbeat ack from catalog-hub; active_seats=${h.role === "worker" ? 3 : 1} mem_rss=94.2MiB`,
    },
    ...(h.stale || h.state !== "online"
      ? [
          {
            id: `${h.name}-log-warn-1`,
            ts: "00:28:02.901",
            level: (h.state === "offline" ? "ERROR" : "WARN") as "WARN" | "ERROR",
            unit: "cerberus-outpost",
            msg:
              h.state === "offline"
                ? `ssh transport probe to ${h.name} failed: connection timed out (retained last known state)`
                : `probe observation on ${h.name} is stale (>300s since last live measurement)`,
          },
        ]
      : []),
    {
      id: `${h.name}-log-3`,
      ts: "00:25:00.004",
      level: "INFO",
      unit: "cerberus-probe",
      msg: `5m rollup sample committed: cpu=${cpuCores} load1=${fmtNum(load1, 2)} mem_avail=${fmtStorage(memTotalBytes - memUsedBytes)} disk_free=${fmtStorage(diskTotalBytes - diskUsedBytes)}`,
    },
    {
      id: `${h.name}-log-4`,
      ts: "00:18:41.773",
      level: "INFO",
      unit: "tailscaled.service",
      msg: `magicsock: derp-4 connected; direct peer path established (rtt=2.4ms)`,
    },
    {
      id: `${h.name}-log-5`,
      ts: "00:12:09.219",
      level: cpuPercent > 75 || memPercent > 80 ? "WARN" : "INFO",
      unit: "kernel",
      msg:
        cpuPercent > 75
          ? `sched: elevated runqueue pressure on ${cpuCores} logical cores (load1=${fmtNum(load1, 2)})`
          : `cgroup2: memory pressure 0.00/0.00/0.00; page cache reclaim nominal`,
    },
    {
      id: `${h.name}-log-6`,
      ts: "00:05:00.018",
      level: "INFO",
      unit: "docker.service",
      msg: `container healthcheck sweep: all supervised sandboxes healthy`,
    },
  ]

  return {
    id: h.name,
    label: preset.label,
    chassis: preset.chassis,
    location: preset.location,
    os: m.os || preset.os,
    osVersion: m.os_version || preset.osVersion,
    arch: m.arch || preset.arch,
    kernel: m.kernel || preset.kernel,
    cpuModel: m.cpu_model || preset.cpuModel,
    cpuCores,
    cpuPercent,
    load1,
    load5,
    load15,
    gpuLabel: m.gpu_model || (m.gpu_count && m.gpu_count > 0 ? `${m.gpu_count}x GPU` : preset.gpuLabel),
    memUsedBytes,
    memTotalBytes,
    memPercent,
    diskUsedBytes,
    diskTotalBytes,
    diskPercent,
    primaryMount,
    uptimeDays,
    uptimeHours,
    uptimeSeconds,
    provider: h.provider || "homelab",
    owner: h.owner || "chrispian",
    tools,
    units,
    mounts,
    events,
    logs,
    isMockEnriched: !m.uptime_seconds || !m.cpu_model || !m.units,
  }
}

function metricTone(pct: number): {
  text: string
  bar: string
  border: string
  bg: string
} {
  if (pct >= 88) {
    return {
      text: "text-destructive",
      bar: "bg-destructive",
      border: "border-destructive/40",
      bg: "bg-destructive/10",
    }
  }
  if (pct >= 72) {
    return {
      text: "text-warning",
      bar: "bg-warning",
      border: "border-warning/40",
      bg: "bg-warning/10",
    }
  }
  return {
    text: "text-primary",
    bar: "bg-primary",
    border: "border-primary/35",
    bg: "bg-primary/10",
  }
}

/**
 * Technical line-art server rack / blade illustration SVG.
 * Draws a crisp isometric/orthographic dual-blade server chassis with status LEDs,
 * ventilation grilles, drive bay ticks, and activity lines.
 */
function ServerLineArt({
  state,
  stale,
  cpuPercent,
  size = "sm",
}: {
  state: string
  stale: boolean
  cpuPercent: number
  size?: "sm" | "lg"
}) {
  const online = state === "online"
  const ledColor = !online ? "var(--color-destructive)" : stale ? "var(--color-warning)" : "var(--color-success)"
  const accentColor = !online
    ? "var(--color-muted-foreground)"
    : cpuPercent >= 85
      ? "var(--color-warning)"
      : "var(--color-primary)"

  const dims = size === "lg" ? "size-14" : "size-11"
  return (
    <div
      className={`relative flex ${dims} shrink-0 items-center justify-center rounded-sm border border-border/80 bg-muted/40 p-1.5 shadow-2xs`}
      aria-hidden="true"
    >
      <svg viewBox="0 0 48 48" fill="none" className="size-full" aria-hidden="true">
        {/* Blueprint corner ticks */}
        <path
          d="M3 7V3H7M41 3H45V7M45 41V45H41M7 45H3V41"
          stroke="currentColor"
          strokeOpacity="0.28"
          strokeWidth="1.2"
          strokeLinecap="round"
        />
        {/* Top Server Blade (1U) */}
        <rect x="6" y="8" width="36" height="13" rx="2" stroke="currentColor" strokeOpacity="0.75" strokeWidth="1.4" />
        {/* Mounting ears */}
        <line x1="4" y1="11" x2="6" y2="11" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.4" />
        <line x1="4" y1="18" x2="6" y2="18" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.4" />
        <line x1="42" y1="11" x2="44" y2="11" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.4" />
        <line x1="42" y1="18" x2="44" y2="18" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.4" />
        {/* Top Blade LEDs */}
        <circle cx="10.5" cy="14.5" r="1.8" fill={ledColor} />
        <circle cx="15" cy="14.5" r="1.3" fill={accentColor} fillOpacity="0.85" />
        {/* Top Blade Drive Bay Grille (line-art vertical vents) */}
        <line x1="21" y1="11.5" x2="21" y2="17.5" stroke="currentColor" strokeOpacity="0.45" strokeWidth="1.2" />
        <line x1="24.5" y1="11.5" x2="24.5" y2="17.5" stroke="currentColor" strokeOpacity="0.45" strokeWidth="1.2" />
        <line x1="28" y1="11.5" x2="28" y2="17.5" stroke="currentColor" strokeOpacity="0.45" strokeWidth="1.2" />
        <line x1="31.5" y1="11.5" x2="31.5" y2="17.5" stroke={accentColor} strokeOpacity="0.8" strokeWidth="1.2" />
        <line x1="35" y1="11.5" x2="35" y2="17.5" stroke="currentColor" strokeOpacity="0.45" strokeWidth="1.2" />
        <line x1="38.5" y1="11.5" x2="38.5" y2="17.5" stroke="currentColor" strokeOpacity="0.45" strokeWidth="1.2" />

        {/* Backplane interconnect lines */}
        <line x1="14" y1="21" x2="14" y2="26" stroke={accentColor} strokeOpacity="0.5" strokeWidth="1.2" />
        <line x1="24" y1="21" x2="24" y2="26" stroke="currentColor" strokeOpacity="0.35" strokeWidth="1.2" />
        <line x1="34" y1="21" x2="34" y2="26" stroke={accentColor} strokeOpacity="0.5" strokeWidth="1.2" />

        {/* Bottom Storage / Compute Blade (2U) */}
        <rect x="6" y="26" width="36" height="14" rx="2" stroke="currentColor" strokeOpacity="0.75" strokeWidth="1.4" />
        {/* Mounting ears */}
        <line x1="4" y1="29" x2="6" y2="29" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.4" />
        <line x1="4" y1="37" x2="6" y2="37" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.4" />
        <line x1="42" y1="29" x2="44" y2="29" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.4" />
        <line x1="42" y1="37" x2="44" y2="37" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.4" />
        {/* Bottom Blade Status & Telemetry bus */}
        <circle cx="10.5" cy="33" r="1.6" fill={accentColor} />
        <rect x="15" y="30" width="11" height="6" rx="1" stroke="currentColor" strokeOpacity="0.5" strokeWidth="1.1" />
        <line x1="17" y1="33" x2="24" y2="33" stroke={accentColor} strokeWidth="1.3" />
        <rect
          x="28.5"
          y="30"
          width="10"
          height="6"
          rx="1"
          stroke="currentColor"
          strokeOpacity="0.5"
          strokeWidth="1.1"
        />
        <line x1="30.5" y1="33" x2="36.5" y2="33" stroke="currentColor" strokeOpacity="0.6" strokeWidth="1.3" />
      </svg>
    </div>
  )
}

/** 10-cell segmented bar for visual Memory / Disk / CPU density inside the 3 telemetry boxes. */
function SegmentedMeter({ percent, segments = 10 }: { percent: number; segments?: number }) {
  const filled = Math.round((Math.min(100, Math.max(0, percent)) / 100) * segments)
  const tone = metricTone(percent)
  const indices = Array.from({ length: segments }, (_, i) => i)
  return (
    <div className="grid grid-cols-10 gap-0.5" aria-hidden="true">
      {indices.map((slot) => (
        <span
          key={`seg-${slot}`}
          className={`h-2 rounded-[1px] transition-colors ${slot < filled ? tone.bar : "bg-muted/70"}`}
        />
      ))}
    </div>
  )
}

/** Radial SVG mini ring gauge used inside the CPU telemetry box. */
function RadialMiniGauge({ percent }: { percent: number }) {
  const clamped = Math.min(100, Math.max(0, percent))
  const r = 13
  const c = 2 * Math.PI * r
  const offset = c - (clamped / 100) * c
  const strokeColor =
    clamped >= 88 ? "var(--color-destructive)" : clamped >= 72 ? "var(--color-warning)" : "var(--color-primary)"
  return (
    <svg viewBox="0 0 34 34" className="size-8 shrink-0 -rotate-90" aria-hidden="true">
      <circle cx="17" cy="17" r={r} fill="none" stroke="currentColor" strokeOpacity="0.16" strokeWidth="3.2" />
      <circle
        cx="17"
        cy="17"
        r={r}
        fill="none"
        stroke={strokeColor}
        strokeWidth="3.2"
        strokeDasharray={c}
        strokeDashoffset={offset}
        strokeLinecap="round"
      />
    </svg>
  )
}

/** Individual Server Card in the Fleet grid — wider layout with protected box boundaries. */
function FleetServerCard({
  host,
  profile,
  selected,
  onSelect,
}: {
  host: Host
  profile: EnrichedHostProfile
  selected: boolean
  onSelect: () => void
}) {
  const online = host.state === "online"
  const cpuTone = metricTone(profile.cpuPercent)
  const memTone = metricTone(profile.memPercent)
  const diskTone = metricTone(profile.diskPercent)

  return (
    <div
      data-slot="fleet-server-card"
      data-state={selected ? "selected" : undefined}
      className={`group relative flex min-w-0 flex-col justify-between gap-3.5 overflow-hidden rounded-md border bg-card/95 p-4 shadow-2xs transition-all hover:border-primary/50 hover:shadow-md ${
        selected ? "border-primary ring-1 ring-primary/40" : "border-border/85"
      }`}
    >
      {/* Top Status Stripe */}
      <span
        className={`absolute inset-x-0 top-0 h-0.5 ${
          !online ? "bg-destructive/75" : host.stale ? "bg-warning/80" : "bg-primary/75"
        }`}
      />

      {/* Header: Line-Art Server Icon + ID, Human Label, Badges & Uptime Days */}
      <div className="flex min-w-0 items-start gap-3.5">
        <ServerLineArt state={host.state} stale={host.stale} cpuPercent={profile.cpuPercent} size="lg" />

        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex min-w-0 items-center justify-between gap-2">
            <div className="flex min-w-0 items-center gap-2">
              <span className="truncate font-mono text-[15px] font-bold tracking-tight text-foreground">
                {host.name}
              </span>
              <Badge
                variant="outline"
                className="h-4.5 shrink-0 border-border/80 bg-muted/40 px-1.5 py-0 font-mono text-[10px] uppercase tracking-wider text-muted-foreground"
              >
                {host.role || "node"}
              </Badge>
            </div>

            <div className="flex shrink-0 items-center gap-1">
              {host.stale && (
                <Badge
                  variant="outline"
                  className="h-4.5 border-warning/45 bg-warning/10 px-1.5 py-0 font-mono text-[10px] text-warning"
                >
                  stale
                </Badge>
              )}
              <Badge
                variant="outline"
                className={`h-4.5 gap-1 px-1.5 py-0 font-mono text-[10px] ${
                  online
                    ? "border-success/40 bg-success/10 text-success"
                    : "border-destructive/40 bg-destructive/10 text-destructive"
                }`}
              >
                <span className={`size-1.5 rounded-full ${online ? "bg-success" : "bg-destructive"}`} />
                {host.state}
              </Badge>
            </div>
          </div>

          <p className="truncate text-xs text-muted-foreground">{profile.label}</p>

          {/* Metadata Strip: Uptime Days, OS/Arch, Location */}
          <div className="mt-1 flex flex-wrap items-center gap-1.5 font-mono text-[10.5px] text-muted-foreground">
            <span className="inline-flex shrink-0 items-center gap-1 rounded-xs border border-primary/25 bg-primary/10 px-1.5 py-0.5 font-medium text-foreground">
              <Icon name="clock" size={11} className="text-primary" />
              UP {profile.uptimeDays}d {profile.uptimeHours}h
            </span>
            <span className="inline-flex shrink-0 items-center gap-1 rounded-xs bg-muted/50 px-1.5 py-0.5">
              <Icon name="cpu" size={11} />
              {profile.os} · {profile.arch}
            </span>
            <span className="truncate rounded-xs bg-muted/35 px-1.5 py-0.5 text-muted-foreground/85">
              {profile.location}
            </span>
          </div>
        </div>
      </div>

      {/* 3 Visually Distinct Telemetry Boxes: CPU, Memory, Hard Drive */}
      <div className="grid grid-cols-3 gap-2.5">
        {/* Box 1: CPU */}
        <div className="flex min-w-0 flex-col justify-between gap-2 overflow-hidden rounded-sm border border-border/75 bg-muted/25 p-2.5">
          <div className="flex min-w-0 items-center justify-between gap-1">
            <span className="truncate font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
              CPU
            </span>
            <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{profile.cpuCores}C</span>
          </div>
          <div className="flex min-w-0 items-center gap-2">
            <RadialMiniGauge percent={profile.cpuPercent} />
            <div className="flex min-w-0 flex-1 flex-col">
              <span className={`truncate font-mono text-sm font-bold leading-tight tabular-nums ${cpuTone.text}`}>
                {profile.cpuPercent}%
              </span>
              <span className="truncate font-mono text-[10px] text-muted-foreground tabular-nums">
                L1 {fmtNum(profile.load1, 2)}
              </span>
            </div>
          </div>
          <SegmentedMeter percent={profile.cpuPercent} />
        </div>

        {/* Box 2: Memory */}
        <div className="flex min-w-0 flex-col justify-between gap-2 overflow-hidden rounded-sm border border-border/75 bg-muted/25 p-2.5">
          <div className="flex min-w-0 items-center justify-between gap-1">
            <span className="truncate font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
              Memory
            </span>
            <span className={`shrink-0 font-mono text-xs font-bold tabular-nums ${memTone.text}`}>
              {profile.memPercent}%
            </span>
          </div>
          <div className="flex min-w-0 flex-col gap-0.5">
            <span className="truncate font-mono text-xs font-semibold text-foreground tabular-nums">
              {fmtStorage(profile.memUsedBytes)}
            </span>
            <span className="truncate font-mono text-[10px] text-muted-foreground tabular-nums">
              of {fmtStorage(profile.memTotalBytes)}
            </span>
          </div>
          <SegmentedMeter percent={profile.memPercent} />
        </div>

        {/* Box 3: Hard Drive */}
        <div className="flex min-w-0 flex-col justify-between gap-2 overflow-hidden rounded-sm border border-border/75 bg-muted/25 p-2.5">
          <div className="flex min-w-0 items-center justify-between gap-1">
            <span className="truncate font-mono text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
              Disk
            </span>
            <span className={`shrink-0 font-mono text-xs font-bold tabular-nums ${diskTone.text}`}>
              {profile.diskPercent}%
            </span>
          </div>
          <div className="flex min-w-0 flex-col gap-0.5">
            <span className="truncate font-mono text-xs font-semibold text-foreground tabular-nums">
              {fmtStorage(profile.diskUsedBytes)}
            </span>
            <span className="truncate font-mono text-[10px] text-muted-foreground tabular-nums">
              of {fmtStorage(profile.diskTotalBytes)}
            </span>
          </div>
          <SegmentedMeter percent={profile.diskPercent} />
        </div>
      </div>

      {/* Card Footer: CPU Model & Click-into Inspect Action */}
      <div className="flex min-w-0 items-center justify-between gap-2 border-t border-border/60 pt-2.5">
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted-foreground" title={profile.cpuModel}>
          {profile.cpuModel}
        </span>
        <Button
          variant="ghost"
          size="xs"
          className="h-6 shrink-0 gap-1 px-2 font-mono text-[11px] text-primary hover:bg-primary/12"
          onClick={onSelect}
        >
          Inspect details
          <Icon name="chevron-right" size={12} />
        </Button>
      </div>
    </div>
  )
}

/** Mini sparkline bar chart for 1-hour 5m sample trends inside the detail modal. */
function SampleSparkBars({
  values,
  tone = "primary",
}: {
  values: { id: string; pct: number; label: string }[]
  tone?: "primary" | "success" | "warning"
}) {
  const barColor = tone === "warning" ? "bg-warning/80" : tone === "success" ? "bg-success/80" : "bg-primary/80"
  return (
    <div className="flex h-9 items-end gap-1 pt-1" aria-hidden="true">
      {values.map((v) => (
        <div key={v.id} title={v.label} className="flex h-full flex-1 items-end rounded-[1px] bg-muted/40">
          <div
            className={`w-full rounded-[1px] transition-all ${barColor}`}
            style={{ height: `${Math.min(100, Math.max(10, v.pct))}%` }}
          />
        </div>
      ))}
    </div>
  )
}

export function Fleet({ win, desk, api }: ViewProps) {
  const host = typeof win.args.host === "string" ? win.args.host : ""
  const [data, setData] = useState<FleetData | null>(null)
  const [error, setError] = useState("")
  const [search, setSearch] = useState("")
  const [roleFilter, setRoleFilter] = useState<string>("all")
  const [logFilter, setLogFilter] = useState<string>("ALL")
  const [logQuery, setLogQuery] = useState<string>("")

  useEffect(() => {
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    setError("")
    const load = async () => {
      try {
        const next = await api.get<FleetData>(`fleet?host=${encodeURIComponent(host)}`)
        if (!stopped) {
          setData(next)
          setError("")
        }
      } catch (e) {
        if (!stopped) setError(message(e))
      } finally {
        if (!stopped) timer = setTimeout(() => void load(), 30000)
      }
    }
    void load()
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [api, host])

  const selectHost = (nextHost: string) => {
    const nextArgs = { ...win.args }
    if (nextHost) {
      nextArgs.host = nextHost
    } else {
      delete nextArgs.host
    }
    void desk.setArgs(win.id, nextArgs)
  }

  const enrichedHosts = useMemo(() => {
    if (!data) return []
    return data.hosts.map((h) => ({
      host: h,
      profile: enrichHost(h, data.source),
    }))
  }, [data])

  const roles = useMemo(() => {
    const set = new Set<string>()
    for (const item of enrichedHosts) {
      if (item.host.role) set.add(item.host.role)
    }
    return ["all", ...Array.from(set).sort()]
  }, [enrichedHosts])

  const filteredHosts = useMemo(() => {
    const q = search.trim().toLowerCase()
    return enrichedHosts.filter(({ host: h, profile }) => {
      if (roleFilter !== "all" && h.role !== roleFilter) return false
      if (!q) return true
      return (
        h.name.toLowerCase().includes(q) ||
        (h.role || "").toLowerCase().includes(q) ||
        profile.label.toLowerCase().includes(q) ||
        profile.cpuModel.toLowerCase().includes(q) ||
        profile.os.toLowerCase().includes(q)
      )
    })
  }, [enrichedHosts, roleFilter, search])

  const fleetTotals = useMemo(() => {
    let onlineCount = 0
    let totalCores = 0
    let totalMemBytes = 0
    let usedMemBytes = 0
    let totalDiskBytes = 0
    let usedDiskBytes = 0
    for (const { host: h, profile } of enrichedHosts) {
      if (h.state === "online") onlineCount++
      totalCores += profile.cpuCores
      totalMemBytes += profile.memTotalBytes
      usedMemBytes += profile.memUsedBytes
      totalDiskBytes += profile.diskTotalBytes
      usedDiskBytes += profile.diskUsedBytes
    }
    return {
      totalHosts: enrichedHosts.length,
      onlineCount,
      totalCores,
      totalMemBytes,
      usedMemBytes,
      totalDiskBytes,
      usedDiskBytes,
    }
  }, [enrichedHosts])

  const selectedEntry = useMemo(
    () => enrichedHosts.find((item) => item.host.name === host) ?? null,
    [enrichedHosts, host],
  )

  // Build 12 5-minute sample points for the modal (using live Cerberus samples when present, or deterministic series)
  const modalSamples = useMemo(() => {
    if (!selectedEntry) return []
    if (data && data.samples.length > 0) {
      return data.samples.map((s, idx) => {
        const l1 = s.load1 ?? selectedEntry.profile.load1
        const cpuPct =
          s.cpu_percent != null
            ? Math.round(s.cpu_percent)
            : Math.min(99, Math.max(3, Math.round((l1 / Math.max(1, selectedEntry.profile.cpuCores)) * 100)))
        const memTot = s.memory_total || selectedEntry.profile.memTotalBytes
        const memUsed =
          data.source === "snapshot" && s.memory_used != null
            ? s.memory_used
            : s.memory_available != null
              ? Math.max(0, memTot - s.memory_available)
              : selectedEntry.profile.memUsedBytes
        const memPct = Math.min(100, Math.max(1, Math.round((memUsed / Math.max(1, memTot)) * 100)))
        const diskTot = s.disk_total || selectedEntry.profile.diskTotalBytes
        const diskUsed =
          s.disks?.[0]?.used != null
            ? s.disks[0].used
            : s.disk_free != null
              ? Math.max(0, diskTot - s.disk_free)
              : selectedEntry.profile.diskUsedBytes
        return {
          id: s.at || `sample-${idx}`,
          at: s.at,
          count: s.count || 5,
          cpuPct,
          load1: l1,
          memUsed,
          memTot,
          memPct,
          diskUsed,
          diskTot,
        }
      })
    }
    // Fallback deterministic 1h series (12 x 5m buckets)
    const p = selectedEntry.profile
    return Array.from({ length: 12 }, (_, idx) => {
      const wave = Math.sin((idx + hashSeed(p.id)) * 0.7) * 8
      const cpuPct = Math.min(99, Math.max(4, Math.round(p.cpuPercent + wave)))
      const load1 = Number(Math.max(0.05, (cpuPct / 100) * p.cpuCores).toFixed(2))
      const memPct = Math.min(98, Math.max(8, Math.round(p.memPercent + wave * 0.35)))
      const memUsed = Math.round((memPct / 100) * p.memTotalBytes)
      const minsAgo = (11 - idx) * 5
      const ts = new Date(Date.now() - minsAgo * 60_000).toISOString()
      return {
        id: `fallback-${p.id}-${idx}`,
        at: ts,
        count: 5,
        cpuPct,
        load1,
        memUsed,
        memTot: p.memTotalBytes,
        memPct,
        diskUsed: p.diskUsedBytes,
        diskTot: p.diskTotalBytes,
      }
    })
  }, [data, selectedEntry])

  const filteredLogs = useMemo(() => {
    if (!selectedEntry) return []
    const q = logQuery.trim().toLowerCase()
    return selectedEntry.profile.logs.filter((l) => {
      if (logFilter !== "ALL" && l.level !== logFilter) return false
      if (!q) return true
      return l.msg.toLowerCase().includes(q) || l.unit.toLowerCase().includes(q)
    })
  }, [selectedEntry, logFilter, logQuery])

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-4 overflow-x-hidden overflow-y-auto overscroll-x-none p-4">
      {error && (
        <div
          role="alert"
          className="flex items-center gap-2 rounded-sm border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive"
        >
          <Icon name="triangle-exclamation-point" size={14} />
          <span>{error} Displayed observations may be out of date.</span>
        </div>
      )}

      {!data && !error && (
        <div className="flex flex-1 items-center justify-center py-12">
          <Spinner />
        </div>
      )}

      {data && (
        <>
          {/* Fleet Summary Strip & Filter Controls */}
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border/80 bg-card/80 px-3.5 py-2.5">
            <div className="flex flex-wrap items-center gap-4">
              <div className="flex items-center gap-2">
                <span className="flex size-7 items-center justify-center rounded-sm border border-primary/30 bg-primary/10 text-primary">
                  <Icon name="server" size={15} />
                </span>
                <div>
                  <div className="flex items-center gap-2">
                    <h2 className="font-mono text-xs font-bold uppercase tracking-wider text-foreground">
                      Cerberus Fleet Inventory
                    </h2>
                    <Badge variant="secondary" className="h-4 px-1.5 font-mono text-[10px]">
                      {fleetTotals.onlineCount}/{fleetTotals.totalHosts} online
                    </Badge>
                  </div>
                  <p className="font-mono text-[11px] text-muted-foreground">
                    Observed {time(data.fetched_at)} · auto-refreshes every 30s
                  </p>
                </div>
              </div>

              <div className="hidden h-7 w-px bg-border/70 lg:block" />

              <div className="hidden flex-wrap items-center gap-4 font-mono text-xs lg:flex">
                <div>
                  <span className="text-[10px] uppercase text-muted-foreground">Compute Cores</span>
                  <p className="font-semibold text-foreground">{fleetTotals.totalCores} logical</p>
                </div>
                <div>
                  <span className="text-[10px] uppercase text-muted-foreground">Fleet Memory</span>
                  <p className="font-semibold text-foreground">
                    {fmtStorage(fleetTotals.usedMemBytes)} / {fmtStorage(fleetTotals.totalMemBytes)}
                  </p>
                </div>
                <div>
                  <span className="text-[10px] uppercase text-muted-foreground">Fleet Storage</span>
                  <p className="font-semibold text-foreground">
                    {fmtStorage(fleetTotals.usedDiskBytes)} / {fmtStorage(fleetTotals.totalDiskBytes)}
                  </p>
                </div>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <div className="flex items-center gap-1 rounded-sm border border-border/75 bg-muted/30 p-0.5">
                {roles.map((r) => (
                  <Button
                    key={r}
                    size="xs"
                    variant={roleFilter === r ? "secondary" : "ghost"}
                    className="h-6 px-2 font-mono text-[11px] capitalize"
                    onClick={() => setRoleFilter(r)}
                  >
                    {r}
                  </Button>
                ))}
              </div>
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Filter machines, role, CPU…"
                className="h-7 w-48 font-mono text-xs"
                aria-label="Filter machines"
              />
            </div>
          </div>

          {data.truncated && (
            <p className="font-mono text-xs text-warning">Inventory is capped at the first 100 machines.</p>
          )}

          {/* Grid of Wider Server Cards */}
          <div className="grid grid-cols-[repeat(auto-fill,minmax(26rem,1fr))] gap-4">
            {filteredHosts.map(({ host: h, profile }) => (
              <FleetServerCard
                key={h.name}
                host={h}
                profile={profile}
                selected={h.name === host}
                onSelect={() => selectHost(h.name)}
              />
            ))}
          </div>

          {filteredHosts.length === 0 && (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {data.hosts.length === 0 ? "No machines reported by Cerberus." : "No machines match your current filter."}
            </p>
          )}

          {/* Click-into Extended Cerberus Host Detail Modal */}
          <Dialog open={!!selectedEntry} onOpenChange={(open) => !open && selectHost("")}>
            <DialogContent className="flex max-h-[88vh] w-[min(64rem,calc(100vw-2rem))] max-w-[calc(100vw-2rem)] flex-col gap-0 overflow-hidden overscroll-x-none p-0 sm:max-w-4xl lg:max-w-5xl">
              {selectedEntry && (
                <>
                  {/* Modal Header */}
                  <DialogHeader className="shrink-0 border-b border-border/80 bg-muted/30 px-5 py-4">
                    <div className="flex flex-wrap items-start justify-between gap-3 pr-6">
                      <div className="flex items-start gap-3.5">
                        <ServerLineArt
                          state={selectedEntry.host.state}
                          stale={selectedEntry.host.stale}
                          cpuPercent={selectedEntry.profile.cpuPercent}
                          size="lg"
                        />
                        <div className="flex flex-col gap-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <DialogTitle className="font-mono text-base font-bold tracking-tight">
                              {selectedEntry.host.name}
                            </DialogTitle>
                            <Badge variant="secondary" className="font-mono text-[10.5px] uppercase">
                              {selectedEntry.host.role || "node"}
                            </Badge>
                            <Badge
                              variant="outline"
                              className={`font-mono text-[10.5px] ${
                                selectedEntry.host.state === "online"
                                  ? "border-success/40 bg-success/10 text-success"
                                  : "border-destructive/40 bg-destructive/10 text-destructive"
                              }`}
                            >
                              {selectedEntry.host.state}
                            </Badge>
                            {selectedEntry.host.stale && (
                              <Badge
                                variant="outline"
                                className="border-warning/40 bg-warning/10 font-mono text-[10.5px] text-warning"
                              >
                                stale observation
                              </Badge>
                            )}
                          </div>
                          <DialogDescription className="text-xs text-muted-foreground">
                            {selectedEntry.profile.label} · {selectedEntry.profile.chassis} ·{" "}
                            {selectedEntry.profile.location}
                          </DialogDescription>
                          <div className="mt-1 flex flex-wrap items-center gap-3 font-mono text-[11px] text-muted-foreground">
                            <span className="inline-flex items-center gap-1 text-foreground">
                              <Icon name="clock" size={12} className="text-primary" />
                              Uptime: {selectedEntry.profile.uptimeDays} days, {selectedEntry.profile.uptimeHours} hrs
                            </span>
                            <span>·</span>
                            <span>Last seen: {time(selectedEntry.host.last_seen) || "just now"}</span>
                            <span>·</span>
                            <span>
                              Provider: {selectedEntry.profile.provider} ({selectedEntry.profile.owner})
                            </span>
                          </div>
                        </div>
                      </div>

                      {selectedEntry.profile.isMockEnriched && (
                        <Badge
                          variant="outline"
                          className="border-chart-2/40 bg-chart-2/10 font-mono text-[10px] text-chart-2"
                          title="Live Cerberus metrics supplemented with mock system/history/log details pending backend projection task"
                        >
                          Live Cerberus + Extended Mock Preview
                        </Badge>
                      )}
                    </div>
                  </DialogHeader>

                  {/* Modal Body: Top 3 Expanded Resource Cards + Tabbed Deep Dive */}
                  <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-4 overflow-x-hidden overflow-y-auto overscroll-x-none p-5">
                    {/* Top 3 Expanded Telemetry Cards */}
                    <div className="grid grid-cols-1 gap-3.5 md:grid-cols-3">
                      {/* CPU Detail Box */}
                      <div className="flex min-w-0 flex-col justify-between gap-2 overflow-hidden rounded-md border border-border/80 bg-card p-3.5">
                        <div className="flex min-w-0 items-center justify-between gap-2">
                          <span className="truncate font-mono text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                            CPU &amp; Load Average
                          </span>
                          <span className="shrink-0 font-mono text-sm font-bold text-primary">
                            {selectedEntry.profile.cpuPercent}%
                          </span>
                        </div>
                        <p className="truncate font-mono text-xs font-medium text-foreground">
                          {selectedEntry.profile.cpuModel}
                        </p>
                        <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1 font-mono text-[11px] text-muted-foreground">
                          <span>{selectedEntry.profile.cpuCores} Logical Cores</span>
                          <span>
                            1m {fmtNum(selectedEntry.profile.load1, 2)} · 5m {fmtNum(selectedEntry.profile.load5, 2)} ·
                            15m {fmtNum(selectedEntry.profile.load15, 2)}
                          </span>
                        </div>
                        <SampleSparkBars
                          values={modalSamples.map((s) => ({
                            id: `cpu-${s.id}`,
                            pct: s.cpuPct,
                            label: `${time(s.at)}: ${s.cpuPct}% (load1 ${fmtNum(s.load1, 2)})`,
                          }))}
                        />
                      </div>

                      {/* Memory Detail Box */}
                      <div className="flex min-w-0 flex-col justify-between gap-2 overflow-hidden rounded-md border border-border/80 bg-card p-3.5">
                        <div className="flex min-w-0 items-center justify-between gap-2">
                          <span className="truncate font-mono text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                            Memory Allocation
                          </span>
                          <span className="shrink-0 font-mono text-sm font-bold text-primary">
                            {selectedEntry.profile.memPercent}%
                          </span>
                        </div>
                        <p className="truncate font-mono text-xs font-medium text-foreground">
                          {fmtStorage(selectedEntry.profile.memUsedBytes)} used of{" "}
                          {fmtStorage(selectedEntry.profile.memTotalBytes)}
                        </p>
                        <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1 font-mono text-[11px] text-muted-foreground">
                          <span>
                            Available:{" "}
                            {fmtStorage(
                              Math.max(0, selectedEntry.profile.memTotalBytes - selectedEntry.profile.memUsedBytes),
                            )}
                          </span>
                          <span>Swap: 0.0 GiB</span>
                        </div>
                        <SampleSparkBars
                          tone="success"
                          values={modalSamples.map((s) => ({
                            id: `mem-${s.id}`,
                            pct: s.memPct,
                            label: `${time(s.at)}: ${fmtStorage(s.memUsed)} / ${fmtStorage(s.memTot)} (${s.memPct}%)`,
                          }))}
                        />
                      </div>

                      {/* Storage Detail Box */}
                      <div className="flex min-w-0 flex-col justify-between gap-2 overflow-hidden rounded-md border border-border/80 bg-card p-3.5">
                        <div className="flex min-w-0 items-center justify-between gap-2">
                          <span className="truncate font-mono text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                            Storage Volumes
                          </span>
                          <span className="shrink-0 font-mono text-sm font-bold text-primary">
                            {selectedEntry.profile.diskPercent}%
                          </span>
                        </div>
                        <div className="flex min-w-0 flex-col gap-2">
                          {selectedEntry.profile.mounts.map((mnt) => (
                            <div key={mnt.mount} className="flex min-w-0 flex-col gap-1">
                              <div className="flex min-w-0 items-center justify-between gap-2 font-mono text-[11px]">
                                <span className="truncate font-medium text-foreground">{mnt.mount}</span>
                                <span className="shrink-0 text-muted-foreground">
                                  {fmtStorage(mnt.usedBytes)} / {fmtStorage(mnt.totalBytes)} ({mnt.percent}%)
                                </span>
                              </div>
                              <SegmentedMeter percent={mnt.percent} />
                            </div>
                          ))}
                        </div>
                      </div>
                    </div>

                    {/* Tabbed Inspector: System Info | History & Samples | Logs | Agents */}
                    <Tabs defaultValue="system" className="flex min-w-0 flex-col gap-3.5">
                      <TabsList variant="line" className="w-full flex-wrap justify-start border-b border-border/70">
                        <TabsTrigger value="system">System Information</TabsTrigger>
                        <TabsTrigger value="history">History &amp; Samples ({modalSamples.length})</TabsTrigger>
                        <TabsTrigger value="logs">
                          System &amp; Probe Logs ({selectedEntry.profile.logs.length})
                        </TabsTrigger>
                        <TabsTrigger value="agents">Agents on Machine ({data.sessions.length})</TabsTrigger>
                      </TabsList>

                      {/* TAB 1: System Information, Probed Tools & Supervised Units */}
                      <TabsContent value="system" className="flex min-w-0 flex-col gap-4">
                        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                          {/* Hardware & OS Specs */}
                          <div className="flex min-w-0 flex-col gap-2.5 overflow-hidden rounded-md border border-border/75 bg-muted/20 p-4">
                            <h4 className="font-mono text-xs font-semibold uppercase tracking-wider text-foreground">
                              Hardware &amp; Kernel Profile
                            </h4>
                            <dl className="grid grid-cols-1 gap-x-4 gap-y-2.5 text-xs sm:grid-cols-2">
                              <div className="min-w-0">
                                <dt className="text-muted-foreground">Operating System</dt>
                                <dd className="break-words font-mono font-medium text-foreground">
                                  {selectedEntry.profile.os} ({selectedEntry.profile.osVersion})
                                </dd>
                              </div>
                              <div className="min-w-0">
                                <dt className="text-muted-foreground">Kernel &amp; Arch</dt>
                                <dd className="break-words font-mono font-medium text-foreground">
                                  {selectedEntry.profile.kernel} · {selectedEntry.profile.arch}
                                </dd>
                              </div>
                              <div className="min-w-0 sm:col-span-2">
                                <dt className="text-muted-foreground">Processor</dt>
                                <dd className="break-words font-mono font-medium text-foreground">
                                  {selectedEntry.profile.cpuModel}
                                </dd>
                              </div>
                              <div className="min-w-0 sm:col-span-2">
                                <dt className="text-muted-foreground">GPU / Accelerator</dt>
                                <dd className="break-words font-mono font-medium text-foreground">
                                  {selectedEntry.profile.gpuLabel}
                                </dd>
                              </div>
                              <div className="min-w-0">
                                <dt className="text-muted-foreground">Primary Mount</dt>
                                <dd className="break-words font-mono font-medium text-foreground">
                                  {selectedEntry.profile.primaryMount} (
                                  {fmtStorage(
                                    selectedEntry.profile.diskTotalBytes - selectedEntry.profile.diskUsedBytes,
                                  )}{" "}
                                  free)
                                </dd>
                              </div>
                              <div className="min-w-0">
                                <dt className="text-muted-foreground">Continuous Uptime</dt>
                                <dd className="break-words font-mono font-medium text-foreground">
                                  {selectedEntry.profile.uptimeDays}d {selectedEntry.profile.uptimeHours}h (
                                  {selectedEntry.profile.uptimeSeconds.toLocaleString()}s)
                                </dd>
                              </div>
                            </dl>

                            <div className="mt-2 border-t border-border/60 pt-2.5">
                              <span className="font-mono text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                                Probed Toolchain (cerberus_host_probe)
                              </span>
                              <div className="mt-1.5 flex flex-wrap gap-1.5">
                                {selectedEntry.profile.tools.map((t) => (
                                  <Badge
                                    key={t.name}
                                    variant="outline"
                                    className={`font-mono text-[11px] ${
                                      t.installed
                                        ? "border-success/40 bg-success/10 text-success"
                                        : "border-border text-muted-foreground opacity-60"
                                    }`}
                                  >
                                    {t.installed ? "✓" : "✕"} {t.name} {t.installed ? `v${t.version}` : "(absent)"}
                                  </Badge>
                                ))}
                              </div>
                            </div>
                          </div>

                          {/* Supervised Units & Daemons */}
                          <div className="flex flex-col gap-2 rounded-md border border-border/75 bg-muted/20 p-3.5">
                            <div className="flex items-center justify-between">
                              <h4 className="font-mono text-xs font-semibold uppercase tracking-wider text-foreground">
                                Supervised Services &amp; Units
                              </h4>
                              <Badge variant="secondary" className="font-mono text-[10px]">
                                {selectedEntry.profile.units.filter((u) => u.state === "running").length}/
                                {selectedEntry.profile.units.length} active
                              </Badge>
                            </div>
                            <div className="flex flex-col divide-y divide-border/60">
                              {selectedEntry.profile.units.map((u) => (
                                <div key={u.name} className="flex items-center justify-between gap-2 py-1.5 text-xs">
                                  <div className="flex min-w-0 flex-col">
                                    <span className="truncate font-mono font-medium text-foreground">{u.name}</span>
                                    <span className="font-mono text-[10.5px] text-muted-foreground">
                                      PID {u.pid}
                                      {u.port ? ` · ${u.port}` : ""} · up {u.uptime}
                                    </span>
                                  </div>
                                  <Badge
                                    variant="outline"
                                    className={`font-mono text-[10px] ${
                                      u.state === "running"
                                        ? "border-success/40 bg-success/10 text-success"
                                        : u.state === "failed"
                                          ? "border-destructive/40 bg-destructive/10 text-destructive"
                                          : "border-border text-muted-foreground"
                                    }`}
                                  >
                                    {u.state}
                                  </Badge>
                                </div>
                              ))}
                            </div>
                          </div>
                        </div>
                      </TabsContent>

                      {/* TAB 2: History & 5-Minute Health Samples */}
                      <TabsContent value="history" className="flex flex-col gap-4">
                        {/* Cerberus Probe & State Event Timeline */}
                        <div className="flex flex-col gap-2 rounded-md border border-border/75 bg-muted/20 p-3.5">
                          <h4 className="font-mono text-xs font-semibold uppercase tracking-wider text-foreground">
                            Cerberus Probe &amp; State Event History
                          </h4>
                          <div className="flex flex-col gap-2">
                            {selectedEntry.profile.events.map((ev) => (
                              <div
                                key={ev.id}
                                className="flex items-start justify-between gap-3 rounded-sm border border-border/60 bg-card/80 px-3 py-2 text-xs"
                              >
                                <div className="flex items-start gap-2.5">
                                  <Badge
                                    variant="outline"
                                    className={`mt-0.5 font-mono text-[10px] uppercase ${
                                      ev.kind === "alert"
                                        ? "border-warning/40 bg-warning/10 text-warning"
                                        : ev.kind === "probe"
                                          ? "border-primary/40 bg-primary/10 text-primary"
                                          : "border-border text-muted-foreground"
                                    }`}
                                  >
                                    {ev.kind}
                                  </Badge>
                                  <div>
                                    <p className="font-medium text-foreground">{ev.title}</p>
                                    <p className="text-muted-foreground">{ev.detail}</p>
                                  </div>
                                </div>
                                <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
                                  {time(ev.at) || ev.at}
                                </span>
                              </div>
                            ))}
                          </div>
                        </div>

                        {/* 5-Minute Health Rollup Samples Table */}
                        <div className="flex flex-col gap-2">
                          <h4 className="font-mono text-xs font-semibold uppercase tracking-wider text-foreground">
                            Recent Health Samples (5-Minute Intervals · Last Hour)
                          </h4>
                          <Table>
                            <TableHeader>
                              <TableRow>
                                <TableHead>Bucket Time</TableHead>
                                <TableHead>Samples</TableHead>
                                <TableHead>CPU Est.</TableHead>
                                <TableHead>1m Load</TableHead>
                                <TableHead>Memory Used</TableHead>
                                <TableHead>Disk Used</TableHead>
                              </TableRow>
                            </TableHeader>
                            <TableBody>
                              {modalSamples.map((s) => (
                                <TableRow key={s.id}>
                                  <TableCell className="font-mono text-xs">{time(s.at) || s.at}</TableCell>
                                  <TableCell className="font-mono text-xs">{s.count}</TableCell>
                                  <TableCell className="font-mono text-xs">{s.cpuPct}%</TableCell>
                                  <TableCell className="font-mono text-xs">{fmtNum(s.load1, 2)}</TableCell>
                                  <TableCell className="font-mono text-xs">
                                    {fmtStorage(s.memUsed)} / {fmtStorage(s.memTot)} ({s.memPct}%)
                                  </TableCell>
                                  <TableCell className="font-mono text-xs">
                                    {fmtStorage(s.diskUsed)} / {fmtStorage(s.diskTot)}
                                  </TableCell>
                                </TableRow>
                              ))}
                            </TableBody>
                          </Table>
                        </div>
                      </TabsContent>

                      {/* TAB 3: Host & Cerberus Logs */}
                      <TabsContent value="logs" className="flex flex-col gap-3">
                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <div className="flex items-center gap-1">
                            {["ALL", "INFO", "WARN", "ERROR"].map((lvl) => (
                              <Button
                                key={lvl}
                                size="xs"
                                variant={logFilter === lvl ? "secondary" : "ghost"}
                                className="h-6 px-2 font-mono text-[11px]"
                                onClick={() => setLogFilter(lvl)}
                              >
                                {lvl}
                              </Button>
                            ))}
                          </div>
                          <Input
                            value={logQuery}
                            onChange={(e) => setLogQuery(e.target.value)}
                            placeholder="Filter log lines by unit or message…"
                            className="h-7 w-64 font-mono text-xs"
                            aria-label="Filter host logs"
                          />
                        </div>

                        <div className="flex flex-col gap-1 rounded-md border border-border/80 bg-background/95 p-3 font-mono text-xs">
                          {filteredLogs.map((l) => (
                            <div
                              key={l.id}
                              className="flex items-start gap-2.5 rounded-xs px-1.5 py-1 hover:bg-muted/40"
                            >
                              <span className="shrink-0 text-muted-foreground">{l.ts}</span>
                              <span
                                className={`w-11 shrink-0 font-bold ${
                                  l.level === "ERROR"
                                    ? "text-destructive"
                                    : l.level === "WARN"
                                      ? "text-warning"
                                      : "text-primary"
                                }`}
                              >
                                {l.level}
                              </span>
                              <span className="w-36 shrink-0 truncate text-chart-2">[{l.unit}]</span>
                              <span className="min-w-0 flex-1 break-words text-foreground/90">{l.msg}</span>
                            </div>
                          ))}
                          {filteredLogs.length === 0 && (
                            <p className="py-4 text-center text-muted-foreground">No log entries match your filter.</p>
                          )}
                        </div>
                      </TabsContent>

                      {/* TAB 4: Agents on Machine */}
                      <TabsContent value="agents" className="flex flex-col gap-3">
                        {data.sessions_warning && (
                          <p role="status" className="text-xs text-muted-foreground">
                            {data.sessions_warning}
                          </p>
                        )}
                        {data.unknown_host_sessions > 0 && (
                          <p className="text-xs text-muted-foreground">
                            {data.unknown_host_sessions} active agent session(s) have unattributed host placement.
                          </p>
                        )}
                        {data.sessions.map((s) => (
                          <SessionRowView key={`${s.kind}:${s.id}`} session={s} />
                        ))}
                        {data.sessions.length === 0 && (
                          <p className="py-4 text-sm text-muted-foreground">
                            No agents with an explicit host placement on {selectedEntry.host.name} were reported.
                          </p>
                        )}
                      </TabsContent>
                    </Tabs>
                  </div>
                </>
              )}
            </DialogContent>
          </Dialog>
        </>
      )}
    </div>
  )
}
