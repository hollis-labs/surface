export type Pub = "draft" | "published"

export type RegistryViewMode = "wiki" | "graph" | "atlas"

export interface Review {
  state?: string
  queue_state?: string
  definition_hash?: string
  proposed_by?: string
  reviewed_by?: string
  reviewed_at?: string
  reason?: string
}

export interface Brief {
  id: string
  kind: string
  schema: string
  key?: string
  name: string
  summary?: string
  owner?: string
  subtype?: string
  lifecycle?: string
  authority?: string
  publication?: string
  updated_at?: string
  revision_id?: string
  version_token?: string
  review?: Review
  edges: number
  proposed?: number
  isLocalOverlay?: boolean
}

export interface Kind {
  kind: string
  schemas: string[]
  count: number
  more?: boolean
}

export interface EdgeSpec {
  Type: string
  Inverse: string
  SourceKinds: string[] | null
  TargetKinds: string[] | null
}

export interface Overview {
  publication: Pub
  kinds: Kind[]
  edges?: EdgeSpec[]
  reviews: Brief[]
  diagnostics?: { ready?: boolean; storage?: string; schemas?: string[] }
  total: number
  as_of?: string
  errors?: Record<string, string>
}

export interface Page {
  records: Brief[]
  cursor?: string
  has_more: boolean
}

export interface Ref {
  authority: string
  kind: string
  id: string
  uri?: string
}

export interface Edge {
  id: string
  relation: {
    type: string
    from: Ref
    to: Ref
    status?: string
    source?: string
    weight?: number
  }
  inverse?: string
  target_state?: string
}

export interface Revision {
  revision_id: string
  version_token?: string
  sequence: number
  updated_at?: string
  lifecycle?: string
  publication?: string
  name?: string
}

export interface Detail {
  record: Record<string, unknown> & { payload?: Record<string, unknown>; metadata?: Record<string, unknown> }
  brief: Brief
  publication: Pub
  outgoing: Edge[]
  incoming: Edge[]
  revisions: Revision[]
  more_history?: boolean
  errors?: Record<string, string>
}

export interface Change {
  path: string
  op: "added" | "removed" | "changed"
  before?: string
  after?: string
}

export interface Prop {
  type?: string | string[]
  enum?: unknown[]
  const?: unknown
  pattern?: string
  minLength?: number
  items?: Prop
}

export interface CorrelationBinding {
  id: string
  authority: "ops" | "torque" | "cerberus" | "tether" | "caster" | "registry"
  kind: string
  ref: string
  label: string
  role?: string
}

export interface WikiSynthesizedData {
  overviewMd: string
  architectureMd: string
  runbookMd: string
  operator?: string
  repo?: string
  disposition?: string
  visibility?: string
  interfaces: string[]
  endpoints: string[]
  torqueProjectId?: string
  opsWorkstreamId?: string
  opsTaskId?: string
  opsStage?: string
  hostId?: string
  verifiedBy?: string
  verificationScope?: string
  lastVerified?: string
  correlationBindings: CorrelationBinding[]
}

export interface LocalRecordOverride {
  brief: Brief
  payload: Record<string, unknown>
  metadata?: Record<string, unknown>
  wiki?: {
    overviewMd?: string
    architectureMd?: string
    runbookMd?: string
  }
  addedEdges: Edge[]
  removedEdgeIds: string[]
  addedCorrelations: CorrelationBinding[]
  removedCorrelationIds: string[]
  updatedAt: string
}

export interface DomainClusterMeta {
  id: string
  title: string
  subtitle: string
  icon: string
  accentClass: string
  badgeClass: string
  kinds: string[]
}

export const DOMAIN_CLUSTERS: DomainClusterMeta[] = [
  {
    id: "portfolio",
    title: "Portfolio & Governance",
    subtitle: "Agencies, codebases, frameworks, ownership & Torque project bindings",
    icon: "building-2",
    accentClass: "text-cyan-400 border-cyan-500/30 bg-cyan-500/10",
    badgeClass: "border-cyan-500/30 bg-cyan-500/10 text-cyan-300",
    kinds: ["agency", "project"],
  },
  {
    id: "execution",
    title: "Execution & Correlation",
    subtitle: "Workstreams, cross-system correlations (Ops/Torque/Tether), and operational configs",
    icon: "git-merge",
    accentClass: "text-emerald-400 border-emerald-500/30 bg-emerald-500/10",
    badgeClass: "border-emerald-500/30 bg-emerald-500/10 text-emerald-300",
    kinds: ["workstream", "correlation", "config"],
  },
  {
    id: "infrastructure",
    title: "Infrastructure & Runtime",
    subtitle: "Deployed services, Cerberus fleet hosts, loopback stations & MCP servers",
    icon: "server",
    accentClass: "text-amber-400 border-amber-500/30 bg-amber-500/10",
    badgeClass: "border-amber-500/30 bg-amber-500/10 text-amber-300",
    kinds: ["service", "host", "mcp_server"],
  },
  {
    id: "fabric",
    title: "Agent Fabric & Knowledge",
    subtitle: "Autonomous agents, skills, tools, launch profiles, architecture docs & SOPs",
    icon: "cpu",
    accentClass: "text-violet-400 border-violet-500/30 bg-violet-500/10",
    badgeClass: "border-violet-500/30 bg-violet-500/10 text-violet-300",
    kinds: ["agent", "skill", "tool", "launch_profile", "document", "knowledge"],
  },
]

export const KIND_META: Record<
  string,
  { label: string; plural: string; icon: string; tone: string; description: string; defaultSchema: string }
> = {
  agency: {
    label: "Agency",
    plural: "Agencies",
    icon: "building-2",
    tone: "text-cyan-400 border-cyan-500/30 bg-cyan-500/10",
    description: "Organizational root owning projects, agents, and operational charters.",
    defaultSchema: "agency/1",
  },
  project: {
    label: "Project",
    plural: "Projects",
    icon: "folder-git-2",
    tone: "text-cyan-300 border-cyan-500/30 bg-cyan-500/10",
    description: "Codebase, framework, or application tracked in Torque and deployed across services.",
    defaultSchema: "project/2",
  },
  workstream: {
    label: "Workstream",
    plural: "Workstreams",
    icon: "layers",
    tone: "text-emerald-400 border-emerald-500/30 bg-emerald-500/10",
    description: "Coordinated operational unit binding Torque tasks, Caster plans, and active agent teams.",
    defaultSchema: "workstream/1",
  },
  correlation: {
    label: "Correlation",
    plural: "Correlations",
    icon: "git-merge",
    tone: "text-emerald-300 border-emerald-500/30 bg-emerald-500/10",
    description: "Cross-system nexus linking sessions, workstreams, Torque tasks, hosts, and projects.",
    defaultSchema: "correlation/1",
  },
  config: {
    label: "Config",
    plural: "Configs",
    icon: "sliders-horizontal",
    tone: "text-teal-300 border-teal-500/30 bg-teal-500/10",
    description: "Operational schedule, backup policy, environment configuration, or runtime rule.",
    defaultSchema: "config/1",
  },
  service: {
    label: "Service",
    plural: "Services",
    icon: "radio",
    tone: "text-amber-400 border-amber-500/30 bg-amber-500/10",
    description: "Running daemon, station, HTTP endpoint, or background worker on a fleet host.",
    defaultSchema: "service/1",
  },
  host: {
    label: "Host",
    plural: "Hosts",
    icon: "server",
    tone: "text-amber-300 border-amber-500/30 bg-amber-500/10",
    description: "Physical or virtual compute node monitored by Cerberus Fleet.",
    defaultSchema: "host/1",
  },
  mcp_server: {
    label: "MCP Server",
    plural: "MCP Servers",
    icon: "plug",
    tone: "text-orange-300 border-orange-500/30 bg-orange-500/10",
    description: "Model Context Protocol gateway or tool provider endpoint.",
    defaultSchema: "mcp_server/1",
  },
  agent: {
    label: "Agent",
    plural: "Agents",
    icon: "bot",
    tone: "text-violet-400 border-violet-500/30 bg-violet-500/10",
    description: "Autonomous specialist seat or orchestrator role operating across the fabric.",
    defaultSchema: "agent/1",
  },
  skill: {
    label: "Skill",
    plural: "Skills",
    icon: "sparkles",
    tone: "text-fuchsia-300 border-fuchsia-500/30 bg-fuchsia-500/10",
    description: "Reusable procedural playbook and tool discipline loaded by agents.",
    defaultSchema: "skill/1",
  },
  tool: {
    label: "Tool",
    plural: "Tools",
    icon: "wrench",
    tone: "text-purple-300 border-purple-500/30 bg-purple-500/10",
    description: "Reviewed capability exposed over Tether MCP to agent sessions.",
    defaultSchema: "tool/1",
  },
  launch_profile: {
    label: "Launch Profile",
    plural: "Launch Profiles",
    icon: "rocket",
    tone: "text-indigo-300 border-indigo-500/30 bg-indigo-500/10",
    description: "Harness, model, and capability envelope for spawning agent sessions.",
    defaultSchema: "launch_profile/1",
  },
  document: {
    label: "Document",
    plural: "Documents",
    icon: "book-open",
    tone: "text-sky-300 border-sky-500/30 bg-sky-500/10",
    description: "Architecture specification, SOP, runbook, or operational reference document.",
    defaultSchema: "document/1",
  },
  knowledge: {
    label: "Knowledge",
    plural: "Knowledge",
    icon: "brain",
    tone: "text-blue-300 border-blue-500/30 bg-blue-500/10",
    description: "Durable investigation dossier, decision record, or domain synthesis.",
    defaultSchema: "knowledge/1",
  },
}

export function getKindMeta(kind: string) {
  return (
    KIND_META[kind] ?? {
      label: kind || "Record",
      plural: `${kind || "Record"}s`,
      icon: "boxes",
      tone: "text-cyan-300 border-cyan-500/30 bg-cyan-500/10",
      description: "System Registry entity and operational metadata record.",
      defaultSchema: `${kind || "record"}/1`,
    }
  )
}
