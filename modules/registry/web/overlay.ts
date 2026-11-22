import type {
  Brief,
  CorrelationBinding,
  Detail,
  Edge,
  EdgeSpec,
  LocalRecordOverride,
  Overview,
  WikiSynthesizedData,
} from "./types"
import { getKindMeta } from "./types"

const STORAGE_KEY = "surface.registry.overlay.v2"

/**
 * Rich seed knowledge records & relationships that complement the live Registry
 * so every domain (including correlations, hosts, MCP servers, launch profiles, and core Hollis Labs systems)
 * has rich operational wiki content, cross-system correlations, and multi-hop graph edges out of the box.
 */
interface SeedKnowledgeEntry {
  brief: Brief
  payload: Record<string, unknown>
  metadata?: Record<string, unknown>
  wiki: {
    overviewMd: string
    architectureMd: string
    runbookMd: string
  }
  edges: Edge[]
  correlations: CorrelationBinding[]
}

function makeEdge(fromId: string, type: string, toId: string, inverse?: string, status = "reviewed"): Edge {
  const fromKind = fromId.split(":")[0] || "record"
  const toKind = toId.split(":")[0] || "record"
  return {
    id: `edge:seed:${fromId}:${type}:${toId}`,
    inverse: inverse || inverseFor(type),
    target_state: "resolved",
    relation: {
      type,
      status,
      source: "authored",
      from: { authority: "registry", kind: fromKind, id: fromId },
      to: { authority: "registry", kind: toKind, id: toId },
    },
  }
}

export function inverseFor(type: string): string {
  const map: Record<string, string> = {
    owned_by: "owns",
    owns: "owned_by",
    operated_by: "operates",
    operates: "operated_by",
    part_of: "contains",
    contains: "part_of",
    deployed_as: "deploys",
    deploys: "deployed_as",
    runs_on: "hosts",
    hosts: "runs_on",
    consumes: "consumed_by",
    consumed_by: "consumes",
    correlates_with: "correlates_with",
    documented_by: "documents",
    documents: "documented_by",
    configures: "configured_by",
    configured_by: "configures",
    uses_skill: "used_by",
    exposes_tool: "exposed_by",
  }
  return map[type] ?? `${type} (in)`
}

const PREDICATE_LABELS: Record<string, string> = {
  owned_by: "Owned by",
  owns: "Owns",
  operated_by: "Operated by",
  operates: "Operates",
  part_of: "Part of",
  contains: "Contains",
  deployed_as: "Deployed as",
  deploys: "Deploys",
  runs_on: "Runs on",
  hosts: "Hosts",
  consumes: "Consumes",
  consumed_by: "Used by",
  correlates_with: "Correlated",
  documented_by: "Documented by",
  documents: "Documents",
  configures: "Configures",
  configured_by: "Configured by",
  uses_skill: "Uses skill",
  used_by: "Used by",
  exposes_tool: "Exposes tool",
  exposed_by: "Exposed by",
}

export function humanizePredicate(type: string): string {
  if (PREDICATE_LABELS[type]) return PREDICATE_LABELS[type]
  const cleaned = type.replace(/\(in\)$/, "").trim()
  if (PREDICATE_LABELS[cleaned]) return PREDICATE_LABELS[cleaned]
  return type.replace(/_/g, " ")
}

const UPPER_WORDS = new Set([
  "os",
  "ade",
  "mcp",
  "cli",
  "api",
  "http",
  "ui",
  "ux",
  "sop",
  "db",
  "id",
  "ai",
  "llm",
  "ssh",
])

function prettifySlug(raw: string): string {
  let s = raw.trim()
  s = s.replace(/^registry:/, "")
  s = s.replace(/^msg:\/\/agent\/agent-mux\//, "")
  s = s.replace(/^msg:\/\/agent\//, "")
  s = s.replace(/^caster:\/\/ops\//, "")
  s = s.replace(/^caster:\/\//, "")
  if (/^[a-z_]+:/.test(s)) {
    s = s.split(":").slice(1).join(":")
  }
  // If it already contains spaces and mixed case and no dots/slashes, keep it
  if (s.includes(" ") && !s.includes(".") && !s.includes("/") && !s.includes("_")) {
    return s
  }
  return s
    .replace(/[./_:-]+/g, " ")
    .trim()
    .split(/\s+/)
    .map((w) => {
      const lower = w.toLowerCase()
      if (UPPER_WORDS.has(lower)) return lower.toUpperCase()
      if (/^(prj|cw)$/i.test(w)) return w.toUpperCase()
      return w.charAt(0).toUpperCase() + w.slice(1)
    })
    .join(" ")
}

/**
 * Always returns a clean, human-readable display name for any Registry object ID, URN, or raw name.
 */
export function formatHumanObjectName(idOrRaw: string, catalogById?: Map<string, Brief>): string {
  if (!idOrRaw) return "Unnamed"
  const cleanId = idOrRaw.trim().replace(/^registry:/, "")
  const fromCatalog = catalogById?.get(cleanId)?.name ?? SEED_KNOWLEDGE[cleanId]?.brief.name
  if (fromCatalog) {
    // If the catalog name itself is a raw dot/slug identifier (e.g. "arch.agency-orchestration" or "project:agentrc"), humanize it
    if (
      !fromCatalog.includes(" ") &&
      (fromCatalog.includes(".") || fromCatalog.includes("-") || fromCatalog.includes("_") || fromCatalog.includes(":"))
    ) {
      return prettifySlug(fromCatalog)
    }
    return fromCatalog
  }
  return prettifySlug(cleanId)
}

export const DEFAULT_EDGE_SPECS: EdgeSpec[] = [
  { Type: "owned_by", Inverse: "owns", SourceKinds: null, TargetKinds: ["agency", "agent"] },
  {
    Type: "operated_by",
    Inverse: "operates",
    SourceKinds: ["project", "service", "workstream"],
    TargetKinds: ["agent", "agency"],
  },
  {
    Type: "part_of",
    Inverse: "contains",
    SourceKinds: null,
    TargetKinds: ["project", "agency", "workstream", "service"],
  },
  { Type: "deployed_as", Inverse: "deploys", SourceKinds: ["project"], TargetKinds: ["service", "mcp_server"] },
  { Type: "runs_on", Inverse: "hosts", SourceKinds: ["service", "agent", "mcp_server"], TargetKinds: ["host"] },
  {
    Type: "consumes",
    Inverse: "consumed_by",
    SourceKinds: ["project", "service", "agent"],
    TargetKinds: ["project", "service", "mcp_server", "tool"],
  },
  {
    Type: "correlates_with",
    Inverse: "correlates_with",
    SourceKinds: null,
    TargetKinds: ["correlation", "workstream", "project", "service", "agent"],
  },
  { Type: "documented_by", Inverse: "documents", SourceKinds: null, TargetKinds: ["document", "knowledge"] },
  {
    Type: "configures",
    Inverse: "configured_by",
    SourceKinds: ["config", "launch_profile"],
    TargetKinds: ["service", "host", "agent", "project"],
  },
  { Type: "uses_skill", Inverse: "used_by", SourceKinds: ["agent", "project", "workstream"], TargetKinds: ["skill"] },
  {
    Type: "exposes_tool",
    Inverse: "exposed_by",
    SourceKinds: ["mcp_server", "service", "project"],
    TargetKinds: ["tool"],
  },
]

export const SEED_KNOWLEDGE: Record<string, SeedKnowledgeEntry> = {
  "project:tether": {
    brief: {
      id: "project:tether",
      kind: "project",
      schema: "project/2",
      key: "tether",
      name: "Tether",
      summary: "Agent communication fabric, durable mailbox, and unified MCP tool multiplexer.",
      owner: "agency:hollis-labs",
      lifecycle: "active",
      authority: "registry",
      publication: "draft",
      updated_at: "2026-11-21T17:00:00Z",
      edges: 3,
    },
    payload: {
      id: "tether",
      name: "Tether",
      schema: "project/2",
      lifecycle: "active",
      owner: "agency:hollis-labs",
      repo: "hollis-labs/tether",
    },
    wiki: {
      overviewMd:
        "**Tether** is the communication bus, durable mailbox, and MCP gateway connecting all Agent OS tools and agent sessions.",
      architectureMd:
        "- **Daemon**: `tetherd` listening on `/var/lib/agents/tether/run/tetherd.sock`\n- **Gateway**: `mcp_server:tether-gateway`",
      runbookMd: "- **Check Status**: Inspect `mcp_server:tether-gateway` or run `tether status` on `host:ops-host`.",
    },
    edges: [
      makeEdge("project:tether", "owned_by", "agency:hollis-labs"),
      makeEdge("project:tether", "deployed_as", "mcp_server:tether-gateway"),
    ],
    correlations: [],
  },
  "project:torque": {
    brief: {
      id: "project:torque",
      kind: "project",
      schema: "project/2",
      key: "torque",
      name: "Torque",
      summary: "Autonomous task scheduler, project backlog tracker, and dispatch state machine.",
      owner: "agency:hollis-labs",
      lifecycle: "active",
      authority: "registry",
      publication: "draft",
      updated_at: "2026-11-21T17:00:00Z",
      edges: 2,
    },
    payload: {
      id: "torque",
      name: "Torque",
      schema: "project/2",
      lifecycle: "active",
      owner: "agency:hollis-labs",
      repo: "hollis-labs/torque",
    },
    wiki: {
      overviewMd:
        "**Torque** tracks all prioritized engineering tasks (`CW-*`), projects (`PRJ-*`), quality gates, and autonomous agent dispatches.",
      architectureMd:
        "- **MCP Tools**: `torque_task_list`, `torque_task_get`, `torque_task_create`, `torque_project_list`",
      runbookMd: "- **Backlog Capture**: Use `/blg` or the Operations Kanban board to inspect and file Torque tasks.",
    },
    edges: [makeEdge("project:torque", "owned_by", "agency:hollis-labs")],
    correlations: [],
  },
  "project:cerberus": {
    brief: {
      id: "project:cerberus",
      kind: "project",
      schema: "project/2",
      key: "cerberus",
      name: "Cerberus",
      summary: "Fleet health monitor, host telemetry probe, and watchdog service across Agent OS nodes.",
      owner: "agency:hollis-labs",
      lifecycle: "active",
      authority: "registry",
      publication: "draft",
      updated_at: "2026-11-21T17:00:00Z",
      edges: 3,
    },
    payload: {
      id: "cerberus",
      name: "Cerberus",
      schema: "project/2",
      lifecycle: "active",
      owner: "agency:hollis-labs",
      repo: "hollis-labs/cerberus",
    },
    wiki: {
      overviewMd:
        "**Cerberus** monitors host CPU, memory, disk, service units, and audit probes across all fleet machines (`host:ops-host`).",
      architectureMd: "- **Operated By**: `agent:cerberus`\n- **Monitored Hosts**: `host:ops-host`",
      runbookMd: "- **Fleet Dashboard**: Open **Operations → Fleet** to view live Cerberus host cards and probe logs.",
    },
    edges: [
      makeEdge("project:cerberus", "owned_by", "agency:hollis-labs"),
      makeEdge("project:cerberus", "operated_by", "agent:cerberus"),
    ],
    correlations: [],
  },
  "agent:surface-lead": {
    brief: {
      id: "agent:surface-lead",
      kind: "agent",
      schema: "agent/1",
      key: "surface-lead",
      name: "Surface Lead",
      summary: "Lead engineering & architecture agent responsible for the Surface ADE framework and Surface Desk.",
      owner: "agency:hollis-labs",
      lifecycle: "active",
      authority: "registry",
      publication: "draft",
      updated_at: "2026-11-21T17:10:00Z",
      edges: 2,
    },
    payload: {
      id: "surface-lead",
      name: "Surface Lead",
      schema: "agent/1",
      lifecycle: "active",
      owner: "agency:hollis-labs",
    },
    wiki: {
      overviewMd:
        "**Surface Lead** coordinates architecture, UI registry parity, and module integration for **Surface** (`project:surface`).",
      architectureMd:
        "- **Agency**: `agency:hollis-labs`\n- **Primary Scope**: `project:surface`, `service:surface-desk`",
      runbookMd: "- **Context Slot**: Automatically receives focused window state from Surface Desk.",
    },
    edges: [makeEdge("agent:surface-lead", "owned_by", "agency:hollis-labs")],
    correlations: [],
  },
  "project:surface": {
    brief: {
      id: "project:surface",
      kind: "project",
      schema: "project/2",
      key: "surface",
      name: "Surface",
      summary: "Agentic Desktop Environment (ADE) framework & operational control plane for Hollis Labs.",
      owner: "agency:hollis-labs",
      subtype: "framework",
      lifecycle: "active",
      authority: "registry",
      publication: "draft",
      updated_at: "2026-11-21T17:15:00Z",
      revision_id: "20569fc9471f05538929b84c0ee65733",
      version_token: "20569fc9471f05538929b84c0ee65733",
      edges: 8,
    },
    payload: {
      id: "surface",
      name: "Surface",
      schema: "project/2",
      type: "framework",
      lifecycle: "active",
      owner: "agency:hollis-labs",
      operator: "agent:surface-lead",
      repo: "hollis-labs/surface",
      disposition: "continue",
      visibility: "private",
      interfaces: [
        { type: "lib" },
        { type: "http", endpoint: "http://127.0.0.1:8190" },
        { type: "cli", binary: "desk" },
      ],
      metadata: {
        torque_project_id: "PRJ-20261118-0001",
        ops_workstream: "ops:surface-ade",
        repo_path: "~/src/surface",
      },
    },
    metadata: {
      ops_edited: true,
      ops_editor: "msg://agent/agent-mux/registry-seeder-archie",
      ops_task: "CW-20261121-0060",
      validator: "registry-ops",
    },
    wiki: {
      overviewMd:
        "**Surface** is the Agentic Desktop Environment (ADE) framework powering **Surface Desk** (`:8190`). It unifies real-time operations (`modules/ops`), system truth (`modules/registry`), durable notes (`modules/notes`), interactive decision cards (`surface.cards`), and multi-window agent collaboration into a single workspace.\n\n- **Primary Role**: Source-of-truth human-agent interface across Torque, Tether, Cerberus, Caster, and Tesseract.\n- **Design Language**: Evolved Blade Runner aesthetic — high-contrast technical line-art, semantic telemetry blocks, and zero-friction inline workflows.",
      architectureMd:
        "### Runtime & Module Architecture\n- **Core Kernel (`core/internal/kernel`, `core/internal/desk`)**: Manages window state, agent turns, non-blocking `card_ask` holds, shared event bus, and cross-module `Cite` entity resolution.\n- **First-Party Modules**:\n  - `modules/ops`: Kanban workstreams, Tachyon event-bus activity pulse, Follow-ups, Fleet telemetry, Agent sessions, and Usage quota monitor.\n  - `modules/registry`: System wiki, knowledge graph, correlation linker, and schema ontology browser.\n- **UI Registry (`registry/ui/*`)**: Canonical component primitives synced via `node scripts/ui.mjs sync`.",
      runbookMd:
        "### Operational Commands & Verification\n- **Local Desk Server**: `./bin/desk serve` on `http://127.0.0.1:8190`\n- **Design Preview Gallery**: `GALLERY_PORT=8435 npx vite --config web/vite.gallery.config.ts`\n- **Verification Gate**: `npx biome check --write web modules packages && npm run typecheck && npm run lint && make desk`",
    },
    edges: [
      makeEdge("project:surface", "owned_by", "agency:hollis-labs"),
      makeEdge("project:surface", "operated_by", "agent:surface-lead"),
      makeEdge("project:surface", "deployed_as", "service:surface-desk"),
      makeEdge("project:surface", "consumes", "project:tether"),
      makeEdge("project:surface", "consumes", "project:torque"),
      makeEdge("project:surface", "consumes", "project:cerberus"),
      makeEdge("project:surface", "correlates_with", "correlation:surface-ops-binding"),
      makeEdge("project:surface", "documented_by", "document:arch.agency-orchestration"),
    ],
    correlations: [
      {
        id: "corr-surface-prj",
        authority: "torque",
        kind: "project",
        ref: "PRJ-20261118-0001",
        label: "Torque Project · Surface ADE",
        role: "tracker_project",
      },
      {
        id: "corr-surface-ws",
        authority: "ops",
        kind: "workstream",
        ref: "workstream:01M7Y3VPD7BEED7MYH4H1ZBAX5",
        label: "Phase 1 Critical Path Workstream",
        role: "active_workstream",
      },
      {
        id: "corr-surface-task",
        authority: "torque",
        kind: "task",
        ref: "CW-20261121-0178",
        label: "CW-20261121-0178 · Registry Write API",
        role: "backlog_task",
      },
      {
        id: "corr-surface-host",
        authority: "cerberus",
        kind: "host",
        ref: "host:ops-host",
        label: "Cerberus Host · ops-host",
        role: "deployment_target",
      },
    ],
  },
  "agency:hollis-labs": {
    brief: {
      id: "agency:hollis-labs",
      kind: "agency",
      schema: "agency/1",
      key: "hollis-labs",
      name: "Hollis Labs",
      summary: "Primary engineering and autonomous operations agency owning the Agent OS and Surface portfolio.",
      owner: "user:chrispian",
      lifecycle: "active",
      authority: "registry",
      publication: "draft",
      updated_at: "2026-11-21T16:00:00Z",
      revision_id: "725aa1b9ce591e87f87481d8f16cd8fc",
      version_token: "725aa1b9ce591e87f87481d8f16cd8fc",
      edges: 7,
    },
    payload: {
      id: "hollis-labs",
      name: "Hollis Labs",
      schema: "agency/1",
      owner: "user:chrispian",
      lifecycle: "active",
      charter:
        "Build and operate self-steering agentic software factories, desktop environments, and resilient infrastructure.",
      workspace_root: "~/dev/hollis-labs",
    },
    wiki: {
      overviewMd:
        "**Hollis Labs** is the core engineering and autonomous operations agency. It governs the **Agent OS** control plane, **Surface** ADE, **Tether** communication & MCP gateway, **Torque** work tracker, **Cerberus** fleet monitor, **Caster** execution planner, and **Tesseract** institutional memory.",
      architectureMd:
        "### Agency Governance & Cell Topology\n- **Principal Owner**: `user:chrispian`\n- **Architecture & Conventions Lead**: `agent:archie`\n- **Operations Manager**: `agent:agent-ops`\n- **Fleet & Watchdog Authority**: `agent:cerberus`",
      runbookMd:
        "### Portfolio Conventions\n- All portfolio repositories reside under `~/dev/hollis-labs/{apps,libs,runtimes}`.\n- Every active project registers its ownership (`owned_by -> agency:hollis-labs`), operator agent, and correlation bindings in the Registry.",
    },
    edges: [
      makeEdge("project:surface", "owned_by", "agency:hollis-labs"),
      makeEdge("project:ops-host", "owned_by", "agency:hollis-labs"),
      makeEdge("project:tether", "owned_by", "agency:hollis-labs"),
      makeEdge("project:torque", "owned_by", "agency:hollis-labs"),
      makeEdge("project:cerberus", "owned_by", "agency:hollis-labs"),
      makeEdge("agent:archie", "owned_by", "agency:hollis-labs"),
      makeEdge("agent:agent-ops", "owned_by", "agency:hollis-labs"),
    ],
    correlations: [
      {
        id: "corr-hl-ops",
        authority: "ops",
        kind: "workstream",
        ref: "workstream:01M7WTS6GFK47R5B339NEGZ8T1",
        label: "Agent OS: Ops Bootstrap Workstream",
        role: "portfolio_workstream",
      },
    ],
  },
  "correlation:surface-ops-binding": {
    brief: {
      id: "correlation:surface-ops-binding",
      kind: "correlation",
      schema: "correlation/1",
      key: "surface-ops-binding",
      name: "Surface ADE ↔ Ops / Torque / Fleet Correlation",
      summary:
        "Binds Surface ADE codebase, Torque project PRJ-20261118-0001, Ops workstreams, and Cerberus host telemetry.",
      owner: "agency:hollis-labs",
      lifecycle: "active",
      authority: "registry",
      publication: "draft",
      updated_at: "2026-11-21T17:30:00Z",
      revision_id: "c0ffee1234567890abcdef1234567890",
      version_token: "c0ffee1234567890abcdef1234567890",
      edges: 5,
    },
    payload: {
      schema: "correlation/1",
      id: "surface-ops-binding",
      name: "Surface ADE ↔ Ops / Torque / Fleet Correlation",
      metadata: {
        torque_project_id: "PRJ-20261118-0001",
        workstream_id: "01M7Y3VPD7BEED7MYH4H1ZBAX5",
        repo_path: "~/dev/hollis-labs/apps/surface",
        cerberus_host: "ops-host",
        tether_namespace: "surface",
      },
      links: {
        project: { authority: "registry", kind: "project", id: "project:surface" },
        workstream: { authority: "registry", kind: "workstream", id: "workstream:01M7Y3VPD7BEED7MYH4H1ZBAX5" },
        service: { authority: "registry", kind: "service", id: "service:surface-desk" },
        host: { authority: "registry", kind: "host", id: "host:ops-host" },
      },
    },
    wiki: {
      overviewMd:
        "This **Correlation Object** serves as the cross-system join key between the **Registry** (`project:surface`), **Operations Board** (`workstream:01M7Y3VPD7BEED7MYH4H1ZBAX5`), **Torque Backlog** (`PRJ-20261118-0001`), **Cerberus Fleet** (`host:ops-host`), and **Tether Agent Sessions**.",
      architectureMd:
        "### Cross-System Resolution Rules\n- Matches Torque tasks tagged `surface` or under `PRJ-20261118-0001`.\n- Associates live agent sessions running in `~/dev/hollis-labs/apps/surface` with the Surface Desk service and Ops Kanban cards.",
      runbookMd:
        "### Updating Correlation Links\nUse the **Relationship & Correlation Picker (`git-fork` icon)** in the Dossier header or click **`+ Link`** on any relationship card to attach additional workstreams, services, or agents.",
    },
    edges: [
      makeEdge("correlation:surface-ops-binding", "correlates_with", "project:surface"),
      makeEdge("correlation:surface-ops-binding", "correlates_with", "workstream:01M7Y3VPD7BEED7MYH4H1ZBAX5"),
      makeEdge("correlation:surface-ops-binding", "correlates_with", "service:surface-desk"),
      makeEdge("correlation:surface-ops-binding", "correlates_with", "host:ops-host"),
      makeEdge("correlation:surface-ops-binding", "operated_by", "agent:agent-ops"),
    ],
    correlations: [
      {
        id: "corr-sob-1",
        authority: "torque",
        kind: "project",
        ref: "PRJ-20261118-0001",
        label: "PRJ-20261118-0001 (Surface)",
        role: "torque_project",
      },
      {
        id: "corr-sob-2",
        authority: "torque",
        kind: "task",
        ref: "CW-20261121-0130",
        label: "CW-20261121-0130 (Cerberus Fleet Projection)",
        role: "torque_task",
      },
      {
        id: "corr-sob-3",
        authority: "torque",
        kind: "task",
        ref: "CW-20261121-0178",
        label: "CW-20261121-0178 (Registry Write Routes)",
        role: "torque_task",
      },
    ],
  },
  "service:surface-desk": {
    brief: {
      id: "service:surface-desk",
      kind: "service",
      schema: "service/1",
      key: "service:surface-desk",
      name: "Surface Desk (:8190)",
      summary:
        "Local Surface ADE desktop server hosting Ops, Registry, Notes, Cards, and Agent Workspace on loopback 8190.",
      owner: "agency:hollis-labs",
      lifecycle: "active",
      authority: "registry",
      publication: "draft",
      updated_at: "2026-11-21T17:20:00Z",
      revision_id: "b02fd3d27c182b1f75bcafa65a2f572e",
      version_token: "b02fd3d27c182b1f75bcafa65a2f572e",
      edges: 4,
    },
    payload: {
      id: "service:surface-desk",
      name: "Surface Desk (:8190)",
      schema: "service/1",
      lifecycle: "active",
      owner: "agency:hollis-labs",
      host: "host:ops-host",
      bind: "127.0.0.1:8190",
      protocol: "http",
      binary: "./bin/desk serve",
    },
    wiki: {
      overviewMd:
        "**Surface Desk** is the primary running instance of the Surface ADE framework on `http://127.0.0.1:8190`. It embeds the compiled module bundles and proxies read/write calls to Tether over stdio or HTTP.",
      architectureMd:
        "- **Listen Address**: `127.0.0.1:8190`\n- **Gateway Link**: Connects to `tetherd.sock` via `module/tetherlink`.\n- **State Store**: Local SQLite state in Nanite data directory.",
      runbookMd:
        "- **Build & Restart**: `make desk && ./bin/desk serve`\n- **Health Probe**: `curl -s http://127.0.0.1:8190/api/m/registry/overview`",
    },
    edges: [
      makeEdge("project:surface", "deployed_as", "service:surface-desk"),
      makeEdge("service:surface-desk", "runs_on", "host:ops-host"),
      makeEdge("service:surface-desk", "owned_by", "agency:hollis-labs"),
      makeEdge("service:surface-desk", "consumes", "mcp_server:tether-gateway"),
    ],
    correlations: [
      {
        id: "corr-sdesk-host",
        authority: "cerberus",
        kind: "host",
        ref: "host:ops-host",
        label: "Loopback :8190 on ops-host",
        role: "runtime_host",
      },
    ],
  },
  "host:ops-host": {
    brief: {
      id: "host:ops-host",
      kind: "host",
      schema: "host/1",
      key: "ops-host",
      name: "ops-host (Primary Fabric Host)",
      summary: "Primary Linux fabric host running Tether daemon, Registry, Torque, Cerberus, Caster, and Tesseract.",
      owner: "agency:hollis-labs",
      lifecycle: "active",
      authority: "registry",
      publication: "draft",
      updated_at: "2026-11-21T17:25:00Z",
      revision_id: "h057a1b9ce591e87f87481d8f16cd8fc",
      version_token: "h057a1b9ce591e87f87481d8f16cd8fc",
      edges: 5,
    },
    payload: {
      id: "ops-host",
      name: "ops-host (Primary Fabric Host)",
      schema: "host/1",
      lifecycle: "active",
      owner: "agency:hollis-labs",
      os: "Linux x86_64 (Ubuntu 24.04 LTS)",
      fabric_root: "/var/lib/agents",
      daemon_socket: "unix:/var/lib/agents/tether/run/tetherd.sock",
    },
    wiki: {
      overviewMd:
        "**`host:ops-host`** is the primary compute node for the Agent OS fabric. Monitored continuously by **Cerberus Fleet**, it hosts the Tether MCP gateway daemon (`tetherd`), Registry database, Torque scheduler, and nightly backup configs.",
      architectureMd:
        "- **Fabric Root**: `/var/lib/agents`\n- **Tether Socket**: `unix:/var/lib/agents/tether/run/tetherd.sock`\n- **Backup Policy**: Configured by `config:backup-ops-host-nightly`",
      runbookMd:
        "- **Inspect in Ops Fleet**: Switch to the **Operations → Fleet** tab to view real-time CPU, Memory, Disk telemetry, and Cerberus probe logs for `ops-host`.",
    },
    edges: [
      makeEdge("host:ops-host", "owned_by", "agency:hollis-labs"),
      makeEdge("service:surface-desk", "runs_on", "host:ops-host"),
      makeEdge("service:audio-station", "runs_on", "host:ops-host"),
      makeEdge("mcp_server:tether-gateway", "runs_on", "host:ops-host"),
      makeEdge("config:backup-ops-host-nightly", "configures", "host:ops-host"),
    ],
    correlations: [
      {
        id: "corr-host-cerberus",
        authority: "cerberus",
        kind: "host",
        ref: "ops-host",
        label: "Cerberus Fleet Node · ops-host (Online)",
        role: "fleet_telemetry",
      },
    ],
  },
  "mcp_server:tether-gateway": {
    brief: {
      id: "mcp_server:tether-gateway",
      kind: "mcp_server",
      schema: "mcp_server/1",
      key: "tether-gateway",
      name: "Tether MCP Gateway",
      summary:
        "Unified Model Context Protocol multiplexer & tool catalog routing Registry, Torque, Cerberus, Caster, and Tesseract tools.",
      owner: "agency:hollis-labs",
      lifecycle: "active",
      authority: "registry",
      publication: "draft",
      updated_at: "2026-11-21T17:10:00Z",
      revision_id: "mcp1234567890abcdef1234567890abc",
      version_token: "mcp1234567890abcdef1234567890abc",
      edges: 4,
    },
    payload: {
      id: "tether-gateway",
      name: "Tether MCP Gateway",
      schema: "mcp_server/1",
      lifecycle: "active",
      owner: "agency:hollis-labs",
      transport: "unix-socket / stdio / http",
      address: "unix:/var/lib/agents/tether/run/tetherd.sock",
      catalog_path: "/var/lib/agents/tether/catalog",
    },
    wiki: {
      overviewMd:
        "**Tether MCP Gateway** is the central tool and mailbox router across the Agent OS fabric. Every Surface module (`ops`, `registry`) communicates with backend authorities through `tetherlink`.",
      architectureMd:
        "- **Discovery Modes**: `flat` and `mux_discover`\n- **Authorities Routed**: `registry_*`, `torque_*`, `cerberus_*`, `caster_*`, `tesseract_*`, `mail_*`",
      runbookMd:
        "- **Verify Gateway**: Check Diagnostics (`shield-check` icon in the Registry top bar) to confirm `Ready` status and active schema catalog.",
    },
    edges: [
      makeEdge("mcp_server:tether-gateway", "owned_by", "agency:hollis-labs"),
      makeEdge("mcp_server:tether-gateway", "runs_on", "host:ops-host"),
      makeEdge("service:surface-desk", "consumes", "mcp_server:tether-gateway"),
    ],
    correlations: [],
  },
}

export function loadLocalOverrides(): Record<string, LocalRecordOverride> {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (!raw) return {}
    return JSON.parse(raw) as Record<string, LocalRecordOverride>
  } catch {
    return {}
  }
}

export function saveLocalOverrides(map: Record<string, LocalRecordOverride>): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(map))
  } catch {
    // ignore storage quota errors
  }
}

/**
 * Merges live API Overview with seed knowledge and local overlay records so counts and edge specs are always complete.
 */
export function enrichOverview(o: Overview, overrides: Record<string, LocalRecordOverride>): Overview {
  const byKind = new Map<string, { kind: string; schemas: string[]; count: number; more?: boolean }>()
  for (const k of o.kinds) {
    byKind.set(k.kind, { ...k })
  }

  // Ensure all seed & local override kinds are represented with non-zero counts
  const extraIdsByKind = new Map<string, Set<string>>()
  for (const id of Object.keys(SEED_KNOWLEDGE)) {
    const b = SEED_KNOWLEDGE[id].brief
    if (!extraIdsByKind.has(b.kind)) extraIdsByKind.set(b.kind, new Set())
    extraIdsByKind.get(b.kind)?.add(id)
  }
  for (const id of Object.keys(overrides)) {
    const b = overrides[id].brief
    if (!extraIdsByKind.has(b.kind)) extraIdsByKind.set(b.kind, new Set())
    extraIdsByKind.get(b.kind)?.add(id)
  }

  for (const [k, set] of extraIdsByKind.entries()) {
    const existing = byKind.get(k)
    const meta = getKindMeta(k)
    if (!existing) {
      byKind.set(k, { kind: k, schemas: [meta.defaultSchema], count: set.size })
    } else if (existing.count === 0) {
      existing.count = set.size
    }
  }

  // Merge edge specs
  const edgeMap = new Map<string, EdgeSpec>()
  for (const e of DEFAULT_EDGE_SPECS) edgeMap.set(e.Type, e)
  for (const e of o.edges ?? []) edgeMap.set(e.Type, e)

  const kinds = [...byKind.values()].sort((a, b) => a.kind.localeCompare(b.kind))
  const total = kinds.reduce((acc, k) => acc + k.count, 0)

  return {
    ...o,
    kinds,
    total,
    edges: [...edgeMap.values()],
  }
}

/**
 * Merges a page of records for a kind/search with seed & local overlay records.
 */
export function enrichRecordsPage(
  liveRecords: Brief[],
  opts: { kind?: string; search?: string; lifecycle?: string; owner?: string },
  overrides: Record<string, LocalRecordOverride>,
): Brief[] {
  const byId = new Map<string, Brief>()

  for (const r of liveRecords) {
    byId.set(r.id, r)
  }

  // Add seed records that match the filter
  for (const [id, entry] of Object.entries(SEED_KNOWLEDGE)) {
    const existing = byId.get(id)
    if (existing) {
      byId.set(id, {
        ...existing,
        summary: existing.summary || entry.brief.summary,
        owner: existing.owner || entry.brief.owner,
        edges: Math.max(existing.edges, entry.edges.length),
      })
    } else {
      byId.set(id, entry.brief)
    }
  }

  // Apply user local overrides on top
  for (const [id, ov] of Object.entries(overrides)) {
    const prev = byId.get(id)
    const netEdges = Math.max(0, (prev?.edges ?? ov.brief.edges ?? 0) + ov.addedEdges.length - ov.removedEdgeIds.length)
    byId.set(id, {
      ...(prev ?? ov.brief),
      ...ov.brief,
      edges: netEdges,
      isLocalOverlay: true,
    })
  }

  const qLower = (opts.search ?? "").trim().toLowerCase()
  return [...byId.values()]
    .filter((r) => {
      if (opts.kind && r.kind !== opts.kind) return false
      if (opts.lifecycle && opts.lifecycle !== "*any*" && r.lifecycle !== opts.lifecycle) return false
      if (opts.owner && opts.owner !== "*any*" && r.owner !== opts.owner) return false
      if (qLower) {
        const hay = `${r.id} ${r.name} ${r.summary ?? ""} ${r.owner ?? ""} ${r.kind}`.toLowerCase()
        if (!hay.includes(qLower)) return false
      }
      return true
    })
    .sort((a, b) => a.name.localeCompare(b.name))
}

/**
 * Builds or enriches a Detail object for a record ID, combining live backend data with seed knowledge and local edits.
 */
export function buildEnrichedDetail(
  id: string,
  liveDetail: Detail | null,
  overrides: Record<string, LocalRecordOverride>,
): Detail | null {
  const seed = SEED_KNOWLEDGE[id]
  const ov = overrides[id]

  // Check if any OTHER record has addedEdges pointing to or from `id`
  const crossOutgoing: Edge[] = []
  const crossIncoming: Edge[] = []
  const GlobalRemovedIds = new Set<string>()

  for (const otherOv of Object.values(overrides)) {
    for (const rem of otherOv.removedEdgeIds) GlobalRemovedIds.add(rem)
    for (const e of otherOv.addedEdges) {
      if (e.relation.from.id === id) crossOutgoing.push(e)
      if (e.relation.to.id === id) crossIncoming.push(e)
    }
  }
  for (const seedEntry of Object.values(SEED_KNOWLEDGE)) {
    for (const e of seedEntry.edges) {
      if (e.relation.from.id === id) crossOutgoing.push(e)
      if (e.relation.to.id === id) crossIncoming.push(e)
    }
  }

  if (!liveDetail && !seed && !ov) return null

  const baseBrief: Brief = ov?.brief ??
    liveDetail?.brief ??
    seed?.brief ?? {
      id,
      kind: id.split(":")[0] || "record",
      schema: getKindMeta(id.split(":")[0] || "record").defaultSchema,
      name: id.split(":").slice(1).join(":") || id,
      lifecycle: "active",
      publication: "draft",
      edges: 0,
    }

  const baseRecord: Record<string, unknown> & {
    payload?: Record<string, unknown>
    metadata?: Record<string, unknown>
  } = {
    ...(seed
      ? {
          id: baseBrief.id,
          kind: baseBrief.kind,
          schema: baseBrief.schema,
          payload: seed.payload,
          metadata: seed.metadata,
        }
      : {}),
    ...(liveDetail?.record ?? {}),
  }

  const mergedPayload: Record<string, unknown> = {
    ...(seed?.payload ?? {}),
    ...((liveDetail?.record?.payload as Record<string, unknown> | undefined) ?? {}),
    ...(ov?.payload ?? {}),
    name: baseBrief.name,
    summary: baseBrief.summary,
    owner: baseBrief.owner,
    lifecycle: baseBrief.lifecycle,
  }

  const mergedMetadata: Record<string, unknown> = {
    ...(seed?.metadata ?? {}),
    ...((liveDetail?.record?.metadata as Record<string, unknown> | undefined) ?? {}),
    ...(ov?.metadata ?? {}),
  }

  // Deduplicate outgoing and incoming edges
  const outMap = new Map<string, Edge>()
  for (const e of [...(liveDetail?.outgoing ?? []), ...crossOutgoing, ...(ov?.addedEdges ?? [])]) {
    if (e.relation.from.id !== id) continue
    if (GlobalRemovedIds.has(e.id)) continue
    const key = `${e.relation.type}:${e.relation.to.id}`
    if (!outMap.has(key)) outMap.set(key, e)
  }

  const inMap = new Map<string, Edge>()
  for (const e of [...(liveDetail?.incoming ?? []), ...crossIncoming]) {
    if (e.relation.to.id !== id) continue
    if (GlobalRemovedIds.has(e.id)) continue
    const key = `${e.relation.type}:${e.relation.from.id}`
    if (!inMap.has(key)) inMap.set(key, e)
  }

  // Also synthesize edges from `owner` and `payload.operator` if not already in outgoing
  if (baseBrief.owner?.includes(":") && !baseBrief.owner.startsWith("user:")) {
    const ownerId = baseBrief.owner.replace(/^registry:/, "")
    const key = `owned_by:${ownerId}`
    if (!outMap.has(key)) {
      outMap.set(key, makeEdge(id, "owned_by", ownerId))
    }
  }
  const operatorVal = typeof mergedPayload.operator === "string" ? mergedPayload.operator : undefined
  if (operatorVal?.includes(":") && !operatorVal.startsWith("user:")) {
    const opId = operatorVal.replace(/^registry:/, "")
    const key = `operated_by:${opId}`
    if (!outMap.has(key)) {
      outMap.set(key, makeEdge(id, "operated_by", opId))
    }
  }

  const outgoing = [...outMap.values()]
  const incoming = [...inMap.values()]

  const finalBrief: Brief = {
    ...baseBrief,
    summary:
      baseBrief.summary ||
      seed?.brief.summary ||
      (typeof mergedPayload.description === "string" ? mergedPayload.description : undefined),
    owner: baseBrief.owner || seed?.brief.owner,
    edges: outgoing.length + incoming.length,
    isLocalOverlay: Boolean(ov),
  }

  const revisions = liveDetail?.revisions?.length
    ? liveDetail.revisions
    : [
        {
          revision_id: finalBrief.revision_id ?? "local-rev-1",
          version_token: finalBrief.version_token ?? "local-vt-1",
          sequence: 1,
          updated_at: ov?.updatedAt ?? finalBrief.updated_at ?? new Date().toISOString(),
          lifecycle: finalBrief.lifecycle,
          publication: finalBrief.publication,
          name: finalBrief.name,
        },
      ]

  return {
    record: {
      ...baseRecord,
      id: finalBrief.id,
      kind: finalBrief.kind,
      schema: finalBrief.schema,
      name: finalBrief.name,
      summary: finalBrief.summary,
      owner: finalBrief.owner,
      lifecycle: finalBrief.lifecycle,
      payload: mergedPayload,
      metadata: mergedMetadata,
    },
    brief: finalBrief,
    publication: liveDetail?.publication ?? "draft",
    outgoing,
    incoming,
    revisions,
    more_history: liveDetail?.more_history,
    errors: liveDetail?.errors,
  }
}

/**
 * Extracts and synthesizes rich Wiki Markdown sections, operational telemetry, and correlation bindings
 * from a Detail record.
 */
export function synthesizeWikiData(d: Detail, overrides: Record<string, LocalRecordOverride>): WikiSynthesizedData {
  const b = d.brief
  const rec = d.record
  const payload = (rec.payload ?? {}) as Record<string, unknown>
  const meta = (rec.metadata ?? {}) as Record<string, unknown>
  const payloadMeta = (payload.metadata ?? {}) as Record<string, unknown>
  const seed = SEED_KNOWLEDGE[b.id]
  const ov = overrides[b.id]

  const desc =
    typeof payload.description === "string"
      ? payload.description
      : typeof payload.purpose === "string"
        ? payload.purpose
        : typeof payload.charter === "string"
          ? payload.charter
          : b.summary || ""

  const operator =
    typeof payload.operator === "string"
      ? payload.operator
      : typeof meta.ops_editor === "string"
        ? meta.ops_editor
        : undefined

  const repo =
    typeof payload.repo === "string"
      ? payload.repo
      : typeof payloadMeta.repo_path === "string"
        ? payloadMeta.repo_path
        : undefined

  const disposition = typeof payload.disposition === "string" ? payload.disposition : undefined
  const visibility = typeof payload.visibility === "string" ? payload.visibility : undefined

  const interfaces: string[] = []
  if (Array.isArray(payload.interfaces)) {
    for (const item of payload.interfaces) {
      if (typeof item === "string") interfaces.push(item)
      else if (item && typeof item === "object") {
        const obj = item as Record<string, unknown>
        const t = typeof obj.type === "string" ? obj.type : "interface"
        const ep =
          typeof obj.endpoint === "string"
            ? ` (${obj.endpoint})`
            : typeof obj.binary === "string"
              ? ` (${obj.binary})`
              : ""
        interfaces.push(`${t}${ep}`)
      }
    }
  }

  const endpoints: string[] = []
  if (typeof payload.bind === "string") endpoints.push(payload.bind)
  if (typeof payload.address === "string") endpoints.push(payload.address)
  if (typeof payload.endpoint === "string") endpoints.push(payload.endpoint)

  const torqueProjectId =
    typeof payloadMeta.torque_project_id === "string"
      ? payloadMeta.torque_project_id
      : typeof payload.torque_project_id === "string"
        ? payload.torque_project_id
        : undefined

  const opsWorkstreamId =
    typeof payloadMeta.ops_id === "string"
      ? payloadMeta.ops_id
      : typeof payloadMeta.workstream_id === "string"
        ? payloadMeta.workstream_id
        : typeof payloadMeta.ops_workstream === "string"
          ? payloadMeta.ops_workstream
          : undefined

  const opsTaskId =
    typeof meta.ops_task === "string"
      ? meta.ops_task
      : typeof payloadMeta.ops_task === "string"
        ? payloadMeta.ops_task
        : undefined

  const opsStage = typeof payloadMeta.ops_stage === "string" ? payloadMeta.ops_stage : undefined
  const hostId =
    typeof payload.host === "string"
      ? payload.host
      : typeof payloadMeta.cerberus_host === "string"
        ? `host:${payloadMeta.cerberus_host}`
        : undefined

  // Build correlation bindings from seed, record payload references/links, and local overrides
  const corrMap = new Map<string, CorrelationBinding>()
  for (const c of seed?.correlations ?? []) corrMap.set(c.id, c)

  if (torqueProjectId) {
    corrMap.set(`torque-prj-${torqueProjectId}`, {
      id: `torque-prj-${torqueProjectId}`,
      authority: "torque",
      kind: "project",
      ref: torqueProjectId,
      label: `Torque Project · ${torqueProjectId}`,
      role: "tracker_project",
    })
  }
  if (opsTaskId) {
    corrMap.set(`torque-task-${opsTaskId}`, {
      id: `torque-task-${opsTaskId}`,
      authority: "torque",
      kind: "task",
      ref: opsTaskId,
      label: `Torque Task · ${opsTaskId}`,
      role: "ops_task",
    })
  }
  if (opsWorkstreamId) {
    corrMap.set(`ops-ws-${opsWorkstreamId}`, {
      id: `ops-ws-${opsWorkstreamId}`,
      authority: "ops",
      kind: "workstream",
      ref: opsWorkstreamId.startsWith("workstream:") ? opsWorkstreamId : `ops:${opsWorkstreamId}`,
      label: `Workstream · ${opsWorkstreamId}`,
      role: opsStage ? `stage:${opsStage}` : "workstream",
    })
  }
  if (Array.isArray(payload.project_urns)) {
    for (const urn of payload.project_urns) {
      if (typeof urn === "string") {
        corrMap.set(`urn-${urn}`, {
          id: `urn-${urn}`,
          authority: "registry",
          kind: "project",
          ref: urn,
          label: `Project · ${urn}`,
          role: "project_urn",
        })
      }
    }
  }
  if (Array.isArray(payload.references)) {
    for (const refObj of payload.references) {
      if (refObj && typeof refObj === "object") {
        const r = refObj as Record<string, unknown>
        const refId = typeof r.id === "string" ? r.id : ""
        const auth = (typeof r.authority === "string" ? r.authority : "registry") as CorrelationBinding["authority"]
        const rKind = typeof r.kind === "string" ? r.kind : "ref"
        if (refId) {
          corrMap.set(`ref-${auth}-${refId}`, {
            id: `ref-${auth}-${refId}`,
            authority: auth,
            kind: rKind,
            ref: refId,
            label: `${auth.toUpperCase()} · ${refId}`,
            role: rKind,
          })
        }
      }
    }
  }
  if (payload.links && typeof payload.links === "object") {
    for (const [role, linkVal] of Object.entries(payload.links as Record<string, unknown>)) {
      if (linkVal && typeof linkVal === "object") {
        const lv = linkVal as Record<string, unknown>
        const lid = typeof lv.id === "string" ? lv.id : ""
        const lkind = typeof lv.kind === "string" ? lv.kind : role
        const lauth = (typeof lv.authority === "string" ? lv.authority : "registry") as CorrelationBinding["authority"]
        if (lid) {
          const fullRef = lauth === "registry" && !lid.includes(":") ? `${lkind}:${lid}` : lid
          corrMap.set(`link-${role}-${fullRef}`, {
            id: `link-${role}-${fullRef}`,
            authority: lauth,
            kind: lkind,
            ref: fullRef,
            label: `${role}: ${fullRef}`,
            role,
          })
        }
      }
    }
  }

  for (const c of ov?.addedCorrelations ?? []) corrMap.set(c.id, c)
  for (const remId of ov?.removedCorrelationIds ?? []) corrMap.delete(remId)

  const defaultOverviewMd =
    seed?.wiki.overviewMd ||
    (desc
      ? `${desc}\n\n- **Canonical Identifier**: \`registry:${b.id}\`\n- **Domain Kind**: \`${b.kind}\` (\`${b.schema}\`)\n- **Lifecycle State**: \`${b.lifecycle ?? "active"}\`${b.owner ? `\n- **Governance Owner**: \`${b.owner}\`` : ""}`
      : `**${b.name}** is a registered \`${b.kind}\` entity in the system knowledge graph (\`registry:${b.id}\`).`)

  const defaultArchitectureMd =
    seed?.wiki.architectureMd ||
    [
      `### System Composition & Interfaces`,
      repo ? `- **Repository / Source**: \`${repo}\`` : null,
      operator ? `- **Designated Operator**: \`${operator}\`` : null,
      interfaces.length > 0 ? `- **Exposed Interfaces**: ${interfaces.map((i) => `\`${i}\``).join(", ")}` : null,
      endpoints.length > 0 ? `- **Runtime Endpoints**: ${endpoints.map((e) => `\`${e}\``).join(", ")}` : null,
      disposition ? `- **Portfolio Disposition**: \`${disposition}\` (${visibility ?? "internal"})` : null,
      d.outgoing.length + d.incoming.length > 0
        ? `- **Graph Connectivity**: Linked to **${d.outgoing.length}** outgoing and **${d.incoming.length}** incoming objects in the Registry knowledge graph.`
        : `- **Graph Connectivity**: Standalone record — use the **Relationship Linker** to connect related agencies, projects, services, or workstreams.`,
    ]
      .filter(Boolean)
      .join("\n")

  const defaultRunbookMd =
    seed?.wiki.runbookMd ||
    [
      `### Operational Guidance & Usage`,
      `- **Citation Reference**: Reference this object in notes, workstreams, or chat as \`registry:${b.id}\`.`,
      torqueProjectId ? `- **Torque Project**: Tracked under \`${torqueProjectId}\`.` : null,
      opsTaskId ? `- **Last Operational Task**: \`${opsTaskId}\`.` : null,
      typeof rec.verified_by === "string"
        ? `- **Verification Attestation**: Verified by \`${rec.verified_by}\` (${typeof rec.verification_scope === "string" ? rec.verification_scope : "source_attestation"}).`
        : null,
    ]
      .filter(Boolean)
      .join("\n")

  return {
    overviewMd:
      ov?.wiki?.overviewMd ?? (typeof payload.wiki_overview === "string" ? payload.wiki_overview : defaultOverviewMd),
    architectureMd:
      ov?.wiki?.architectureMd ??
      (typeof payload.wiki_architecture === "string" ? payload.wiki_architecture : defaultArchitectureMd),
    runbookMd:
      ov?.wiki?.runbookMd ?? (typeof payload.wiki_runbook === "string" ? payload.wiki_runbook : defaultRunbookMd),
    operator,
    repo,
    disposition,
    visibility,
    interfaces,
    endpoints,
    torqueProjectId,
    opsWorkstreamId,
    opsTaskId,
    opsStage,
    hostId,
    verifiedBy: typeof rec.verified_by === "string" ? rec.verified_by : undefined,
    verificationScope: typeof rec.verification_scope === "string" ? rec.verification_scope : undefined,
    lastVerified: typeof rec.last_verified === "string" ? rec.last_verified : undefined,
    correlationBindings: [...corrMap.values()],
  }
}
