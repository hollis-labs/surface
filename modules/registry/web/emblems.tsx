import { formatHumanObjectName, humanizePredicate } from "./overlay"
import type { Brief, Edge } from "./types"
import { getKindMeta } from "./types"

/**
 * Evolved Blade Runner technical line-art emblem for Registry domains.
 * Echoes the Fleet ServerLineArt, Agents AgentLineArt, and Usage UsageLineArt.
 */
export function RegistryKindLineArt({
  kind,
  lifecycle = "active",
  size = "md",
}: {
  kind: string
  lifecycle?: string
  size?: "sm" | "md" | "lg"
}) {
  const stroke =
    lifecycle === "deprecated" || lifecycle === "retired"
      ? "#f87171"
      : lifecycle === "experimental"
        ? "#fbbf24"
        : kind === "agency" || kind === "project"
          ? "#22d3ee"
          : kind === "workstream" || kind === "correlation" || kind === "config"
            ? "#34d399"
            : kind === "service" || kind === "host" || kind === "mcp_server"
              ? "#f59e0b"
              : "#a78bfa"

  const dimClass = size === "sm" ? "size-9" : size === "lg" ? "size-14" : "size-12"

  return (
    <div
      className={`relative flex ${dimClass} shrink-0 items-center justify-center rounded-md border border-border/70 bg-background/80 shadow-inner`}
    >
      <svg viewBox="0 0 48 48" className="size-full p-1.5" fill="none" aria-hidden="true">
        {/* Blueprint corner ticks */}
        <path d="M4 10 V4 H10" stroke={stroke} strokeWidth="1" strokeOpacity="0.45" />
        <path d="M38 4 H44 V10" stroke={stroke} strokeWidth="1" strokeOpacity="0.45" />
        <path d="M4 38 V44 H10" stroke={stroke} strokeWidth="1" strokeOpacity="0.45" />
        <path d="M38 44 H44 V38" stroke={stroke} strokeWidth="1" strokeOpacity="0.45" />

        {kind === "agency" ? (
          <>
            <polygon
              points="24,7 40,16 40,32 24,41 8,32 8,16"
              stroke={stroke}
              strokeWidth="1.4"
              fill={stroke}
              fillOpacity="0.08"
            />
            <circle cx="24" cy="24" r="6" stroke={stroke} strokeWidth="1.4" />
            <line x1="24" y1="7" x2="24" y2="18" stroke={stroke} strokeWidth="1.1" strokeDasharray="2 2" />
            <line x1="40" y1="32" x2="29" y2="27" stroke={stroke} strokeWidth="1.1" strokeDasharray="2 2" />
            <line x1="8" y1="32" x2="19" y2="27" stroke={stroke} strokeWidth="1.1" strokeDasharray="2 2" />
            <circle cx="24" cy="24" r="2" fill={stroke} />
          </>
        ) : kind === "project" ? (
          <>
            <rect
              x="8"
              y="10"
              width="32"
              height="28"
              rx="3"
              stroke={stroke}
              strokeWidth="1.4"
              fill={stroke}
              fillOpacity="0.07"
            />
            <line x1="8" y1="17" x2="40" y2="17" stroke={stroke} strokeWidth="1.2" strokeOpacity="0.7" />
            <circle cx="13" cy="13.5" r="1.3" fill={stroke} />
            <circle cx="17.5" cy="13.5" r="1.3" fill={stroke} fillOpacity="0.6" />
            <path d="M15 24 L20 27.5 L15 31" stroke={stroke} strokeWidth="1.5" strokeLinecap="round" />
            <line x1="23" y1="31" x2="33" y2="31" stroke={stroke} strokeWidth="1.5" strokeLinecap="round" />
          </>
        ) : kind === "workstream" || kind === "correlation" ? (
          <>
            <circle cx="12" cy="14" r="4" stroke={stroke} strokeWidth="1.4" fill={stroke} fillOpacity="0.12" />
            <circle cx="36" cy="14" r="4" stroke={stroke} strokeWidth="1.4" fill={stroke} fillOpacity="0.12" />
            <circle cx="24" cy="35" r="4.5" stroke={stroke} strokeWidth="1.5" fill={stroke} fillOpacity="0.18" />
            <path d="M15.5 16.5 L21.5 31" stroke={stroke} strokeWidth="1.3" />
            <path d="M32.5 16.5 L26.5 31" stroke={stroke} strokeWidth="1.3" />
            <line x1="16" y1="14" x2="32" y2="14" stroke={stroke} strokeWidth="1.1" strokeDasharray="2 2" />
            <circle cx="24" cy="22" r="2" fill={stroke} />
          </>
        ) : kind === "service" || kind === "host" || kind === "mcp_server" ? (
          <>
            <rect
              x="9"
              y="9"
              width="30"
              height="9"
              rx="2"
              stroke={stroke}
              strokeWidth="1.3"
              fill={stroke}
              fillOpacity="0.08"
            />
            <rect
              x="9"
              y="20.5"
              width="30"
              height="9"
              rx="2"
              stroke={stroke}
              strokeWidth="1.3"
              fill={stroke}
              fillOpacity="0.08"
            />
            <rect x="13" y="32" width="22" height="6" rx="1.5" stroke={stroke} strokeWidth="1.2" strokeOpacity="0.7" />
            <circle cx="14" cy="13.5" r="1.5" fill={stroke} />
            <circle cx="14" cy="25" r="1.5" fill={stroke} />
            <line x1="22" y1="13.5" x2="34" y2="13.5" stroke={stroke} strokeWidth="1.2" strokeOpacity="0.6" />
            <line x1="22" y1="25" x2="34" y2="25" stroke={stroke} strokeWidth="1.2" strokeOpacity="0.6" />
          </>
        ) : kind === "agent" || kind === "skill" || kind === "tool" ? (
          <>
            <circle
              cx="24"
              cy="24"
              r="11"
              stroke={stroke}
              strokeWidth="1.4"
              strokeDasharray="4 2"
              fill={stroke}
              fillOpacity="0.07"
            />
            <polygon points="24,15 32,24 24,33 16,24" stroke={stroke} strokeWidth="1.4" />
            <circle cx="24" cy="24" r="2.5" fill={stroke} />
            <line x1="24" y1="7" x2="24" y2="13" stroke={stroke} strokeWidth="1.2" />
            <line x1="24" y1="35" x2="24" y2="41" stroke={stroke} strokeWidth="1.2" />
            <line x1="7" y1="24" x2="13" y2="24" stroke={stroke} strokeWidth="1.2" />
            <line x1="35" y1="24" x2="41" y2="24" stroke={stroke} strokeWidth="1.2" />
          </>
        ) : (
          <>
            <path d="M12 9 H30 L36 15 V39 H12 Z" stroke={stroke} strokeWidth="1.4" fill={stroke} fillOpacity="0.08" />
            <path d="M30 9 V15 H36" stroke={stroke} strokeWidth="1.2" />
            <line x1="17" y1="21" x2="31" y2="21" stroke={stroke} strokeWidth="1.3" />
            <line x1="17" y1="26.5" x2="31" y2="26.5" stroke={stroke} strokeWidth="1.3" strokeOpacity="0.75" />
            <line x1="17" y1="32" x2="25" y2="32" stroke={stroke} strokeWidth="1.3" strokeOpacity="0.55" />
          </>
        )}
      </svg>
    </div>
  )
}

/**
 * Interactive SVG Knowledge Graph Constellation visualizer for the active record and its 1-hop / 2-hop neighborhood.
 */
export function KnowledgeGraphCanvas({
  center,
  outgoing,
  incoming,
  catalogById,
  onSelectId,
}: {
  center: Brief
  outgoing: Edge[]
  incoming: Edge[]
  catalogById: Map<string, Brief>
  onSelectId: (id: string) => void
}) {
  // Build unique neighbor nodes around the center node
  const neighbors: {
    slot: string
    id: string
    name: string
    kind: string
    predicate: string
    direction: "out" | "in" | "both"
    status?: string
    angle: number
    radius: number
  }[] = []

  const seen = new Map<string, { predicate: string; direction: "out" | "in" | "both"; status?: string }>()
  for (const e of outgoing) {
    const targetId = e.relation.to.id
    if (targetId === center.id) continue
    seen.set(targetId, { predicate: e.relation.type, direction: "out", status: e.relation.status })
  }
  for (const e of incoming) {
    const sourceId = e.relation.from.id
    if (sourceId === center.id) continue
    const prev = seen.get(sourceId)
    if (prev) {
      seen.set(sourceId, {
        predicate: `${prev.predicate} / ${e.inverse || e.relation.type}`,
        direction: "both",
        status: prev.status || e.relation.status,
      })
    } else {
      seen.set(sourceId, {
        predicate: e.inverse || `${e.relation.type} (in)`,
        direction: "in",
        status: e.relation.status,
      })
    }
  }

  const entries = [...seen.entries()].slice(0, 14)
  const total = Math.max(entries.length, 1)
  entries.forEach(([nodeId, info], idx) => {
    const b = catalogById.get(nodeId)
    const kind = b?.kind ?? (nodeId.includes(":") ? nodeId.split(":")[0] : "record")
    const name = formatHumanObjectName(nodeId, catalogById)
    const angle = (idx / total) * Math.PI * 2 - Math.PI / 2
    const radius = entries.length > 7 ? (idx % 2 === 0 ? 155 : 215) : 175
    neighbors.push({
      slot: `node-${nodeId}`,
      id: nodeId,
      name,
      kind,
      predicate: humanizePredicate(info.predicate),
      direction: info.direction,
      status: info.status,
      angle,
      radius,
    })
  })

  const cx = 360
  const cy = 230

  const strokeForKind = (k: string) => {
    if (k === "agency" || k === "project") return "#22d3ee"
    if (k === "workstream" || k === "correlation" || k === "config") return "#34d399"
    if (k === "service" || k === "host" || k === "mcp_server") return "#f59e0b"
    return "#a78bfa"
  }

  const centerColor = strokeForKind(center.kind)

  return (
    <div className="relative flex flex-col overflow-hidden rounded-lg border border-border/70 bg-background/60">
      <div className="flex items-center justify-between border-b border-border/60 bg-muted/20 px-4 py-2">
        <div className="flex items-center gap-2">
          <span className="size-2 rounded-full bg-cyan-400 shadow-[0_0_8px_#22d3ee]" />
          <span className="font-mono text-xs font-semibold tracking-wider text-foreground uppercase">
            Knowledge Graph Constellation · {center.name}
          </span>
        </div>
        <div className="flex items-center gap-3 text-[11px] text-muted-foreground">
          <span className="inline-flex items-center gap-1">
            <span className="size-2 rounded-full bg-cyan-400" /> Portfolio
          </span>
          <span className="inline-flex items-center gap-1">
            <span className="size-2 rounded-full bg-emerald-400" /> Execution & Correlation
          </span>
          <span className="inline-flex items-center gap-1">
            <span className="size-2 rounded-full bg-amber-400" /> Infrastructure
          </span>
          <span className="inline-flex items-center gap-1">
            <span className="size-2 rounded-full bg-violet-400" /> Agent Fabric & Docs
          </span>
        </div>
      </div>

      <div className="relative flex items-center justify-center p-2">
        <svg viewBox="0 0 720 460" className="h-[26rem] w-full max-w-4xl select-none" aria-hidden="true">
          <defs>
            <radialGradient id="regGraphGlow" cx="50%" cy="50%" r="50%">
              <stop offset="0%" stopColor={centerColor} stopOpacity="0.16" />
              <stop offset="70%" stopColor={centerColor} stopOpacity="0.03" />
              <stop offset="100%" stopColor={centerColor} stopOpacity="0" />
            </radialGradient>
          </defs>

          {/* Subtle orbital rings */}
          <circle cx={cx} cy={cy} r="235" fill="url(#regGraphGlow)" />
          <circle
            cx={cx}
            cy={cy}
            r="175"
            fill="none"
            stroke="#334155"
            strokeWidth="1"
            strokeDasharray="3 5"
            strokeOpacity="0.55"
          />
          <circle
            cx={cx}
            cy={cy}
            r="105"
            fill="none"
            stroke="#334155"
            strokeWidth="1"
            strokeDasharray="2 4"
            strokeOpacity="0.35"
          />

          {/* Edges from center to neighbors */}
          {neighbors.map((n) => {
            const nx = cx + Math.cos(n.angle) * n.radius
            const ny = cy + Math.sin(n.angle) * n.radius
            const mx = (cx + nx) / 2
            const my = (cy + ny) / 2
            const col = strokeForKind(n.kind)
            return (
              <g key={`edge-${n.slot}`}>
                <line
                  x1={cx}
                  y1={cy}
                  x2={nx}
                  y2={ny}
                  stroke={col}
                  strokeWidth={n.status === "proposed" ? "1.2" : "1.6"}
                  strokeOpacity={n.status === "proposed" ? "0.45" : "0.65"}
                  strokeDasharray={n.status === "proposed" ? "4 3" : undefined}
                />
                <rect
                  x={mx - 42}
                  y={my - 9}
                  width="84"
                  height="17"
                  rx="4"
                  fill="#090d16"
                  fillOpacity="0.9"
                  stroke={col}
                  strokeWidth="0.7"
                  strokeOpacity="0.45"
                />
                <text
                  x={mx}
                  y={my + 3}
                  textAnchor="middle"
                  fill="#94a3b8"
                  fontSize="9"
                  fontFamily="ui-monospace, SFMono-Regular, monospace"
                >
                  {n.predicate.length > 14 ? `${n.predicate.slice(0, 13)}…` : n.predicate}
                </text>
              </g>
            )
          })}

          {/* Neighbor nodes */}
          {neighbors.map((n) => {
            const nx = cx + Math.cos(n.angle) * n.radius
            const ny = cy + Math.sin(n.angle) * n.radius
            const col = strokeForKind(n.kind)
            const meta = getKindMeta(n.kind)
            return (
              <g
                key={n.slot}
                className="cursor-pointer transition-opacity hover:opacity-90"
                onClick={() => onSelectId(n.id)}
              >
                <circle cx={nx} cy={ny} r="24" fill="#0b111e" stroke={col} strokeWidth="1.6" />
                <circle cx={nx} cy={ny} r="19" fill={col} fillOpacity="0.12" />
                <text
                  x={nx}
                  y={ny - 2}
                  textAnchor="middle"
                  fill={col}
                  fontSize="8.5"
                  fontWeight="600"
                  fontFamily="ui-monospace, SFMono-Regular, monospace"
                >
                  {meta.label.toUpperCase()}
                </text>
                <text x={nx} y={ny + 8} textAnchor="middle" fill="#e2e8f0" fontSize="8">
                  {n.direction === "out" ? "→ out" : n.direction === "in" ? "← in" : "↔ bi"}
                </text>
                <rect
                  x={nx - 62}
                  y={ny + 27}
                  width="124"
                  height="18"
                  rx="4"
                  fill="#090d16"
                  fillOpacity="0.88"
                  stroke={col}
                  strokeWidth="0.7"
                  strokeOpacity="0.4"
                />
                <text x={nx} y={ny + 39} textAnchor="middle" fill="#f1f5f9" fontSize="10" fontWeight="500">
                  {n.name.length > 18 ? `${n.name.slice(0, 17)}…` : n.name}
                </text>
              </g>
            )
          })}

          {/* Center active node */}
          <g>
            <circle cx={cx} cy={cy} r="44" fill="#090d16" stroke={centerColor} strokeWidth="2.2" />
            <circle cx={cx} cy={cy} r="37" fill={centerColor} fillOpacity="0.15" />
            <text
              x={cx}
              y={cy - 10}
              textAnchor="middle"
              fill={centerColor}
              fontSize="9.5"
              fontWeight="700"
              fontFamily="ui-monospace, SFMono-Regular, monospace"
            >
              {center.kind.toUpperCase()}
            </text>
            <text x={cx} y={cy + 5} textAnchor="middle" fill="#f8fafc" fontSize="12" fontWeight="700">
              {center.name.length > 14 ? `${center.name.slice(0, 13)}…` : center.name}
            </text>
            <text
              x={cx}
              y={cy + 19}
              textAnchor="middle"
              fill="#94a3b8"
              fontSize="8.5"
              fontFamily="ui-monospace, SFMono-Regular, monospace"
            >
              {neighbors.length} linked
            </text>
          </g>
        </svg>
      </div>
    </div>
  )
}
