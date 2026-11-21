/**
 * Activity Timeline window (`surface.activity` / `surface/activity`):
 * A structured vertical timeline of agent tool calls, card interactions, and context steps,
 * opened when clicking the collapsed activity line in the chat stream or from the Agent header.
 *
 * Follows the "Action in front, details behind drilldown" principle: each node shows a
 * human-readable summary, semantic category icon & color, topic/turn tag, and status,
 * expanding on click to reveal the raw JSON input and output payload.
 */
import { type ChatItem, useChat } from "@hollis-labs/surface-chat"
import { useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Icon, type ViewProps } from "../sdk"
import { agentSession } from "./AgentPanel"
import type { ToolItem } from "./items"

export type ActivityCategory = "read" | "write" | "card" | "desk" | "system"

export interface TimelineEntry {
  id: string
  tool: string
  summary: string
  detail?: string
  category: ActivityCategory
  status: "running" | "done" | "error"
  topic: string
  turnLabel: string
  input?: unknown
  result?: string
}

/** Semantic category and human-readable summary for a tool call. */
export function describeToolCall(
  name: string,
  rawInput?: unknown,
): {
  category: ActivityCategory
  icon: string
  summary: string
  detail?: string
} {
  const input = (rawInput && typeof rawInput === "object" ? rawInput : {}) as Record<string, unknown>
  const str = (k: string) => (typeof input[k] === "string" ? String(input[k]) : "")

  if (name === "card_ask" || name === "card_show" || name === "card_update" || name === "genui_show") {
    const kind = str("kind") || str("pattern") || "card"
    const title = str("title") || ((input.params as Record<string, unknown> | undefined)?.title as string) || ""
    return {
      category: "card",
      icon: "gallery-vertical-end",
      summary: name === "card_ask" ? `Asked via ${kind} card` : `Displayed ${kind} surface`,
      detail: title || undefined,
    }
  }
  if (name.startsWith("desk_") || name.startsWith("layout.") || name.startsWith("window.")) {
    const view = str("view") || str("layout") || str("title") || str("window")
    return {
      category: "desk",
      icon: "layout-dashboard",
      summary: `Desk action · ${name.replace(/^desk_/, "")}`,
      detail: view || undefined,
    }
  }
  if (
    name.includes("read") ||
    name.includes("list") ||
    name.includes("get") ||
    name.includes("search") ||
    name.includes("recall") ||
    name.includes("discover") ||
    name.includes("inspect")
  ) {
    const target = str("path") || str("query") || str("q") || str("place") || str("id") || str("ref")
    return {
      category: "read",
      icon: "search",
      summary: `Inspected · ${name}`,
      detail: target || undefined,
    }
  }
  if (
    name.includes("write") ||
    name.includes("edit") ||
    name.includes("put") ||
    name.includes("create") ||
    name.includes("delete") ||
    name.includes("move") ||
    name.includes("mkdir") ||
    name.includes("save") ||
    name.includes("bash") ||
    name.includes("run")
  ) {
    const target = str("path") || str("command") || str("to") || str("title") || str("name")
    return {
      category: "write",
      icon: "terminal",
      summary: `Executed · ${name}`,
      detail: target || undefined,
    }
  }
  return {
    category: "system",
    icon: "cpu",
    summary: `Ran ${name}`,
    detail: str("path") || str("id") || str("title") || undefined,
  }
}

/** Infer a short topic slug from a user prompt or tool context. */
export function inferTopicFromText(text: string, fallback = "general"): string {
  const clean = text.trim().toLowerCase()
  if (!clean) return fallback
  if (clean.includes("card") || clean.includes("drawer") || clean.includes("wizard")) return "cards-ux"
  if (clean.includes("context") || clean.includes("inspector") || clean.includes("slot")) return "agent-context"
  if (clean.includes("theme") || clean.includes("color") || clean.includes("blade") || clean.includes("markdown"))
    return "theme-polish"
  if (clean.includes("window") || clean.includes("float") || clean.includes("popout") || clean.includes("layout"))
    return "window-mgr"
  if (clean.includes("file") || clean.includes("folder")) return "files"
  if (clean.includes("note") || clean.includes("doc") || clean.includes("draft")) return "notes"
  if (clean.includes("repo") || clean.includes("github") || clean.includes("review")) return "repo-review"
  if (clean.includes("arch") || clean.includes("design") || clean.includes("workflow")) return "architecture"
  const words = clean
    .replace(/[^a-z0-9\s-]/g, " ")
    .split(/\s+/)
    .filter(
      (w) => w.length > 3 && !["that", "this", "with", "from", "have", "what", "when", "make", "want"].includes(w),
    )
  if (words.length >= 2) return `${words[0]}-${words[1]}`.slice(0, 20)
  return words[0]?.slice(0, 18) || fallback
}

const SAMPLE_ENTRIES: TimelineEntry[] = [
  {
    id: "sample-1",
    tool: "tesseract_recall",
    summary: "Inspected · tesseract_recall",
    detail: "namespace: surface/architecture · limit: 5",
    category: "read",
    status: "done",
    topic: "architecture",
    turnLabel: "Turn #1",
    input: { namespace: "surface/architecture", query: "card hold and activity timeline", limit: 5 },
    result: '{"matches": 3, "top": "ADR-014: Non-blocking card lifecycle & activity stream"}',
  },
  {
    id: "sample-2",
    tool: "files_list",
    summary: "Inspected · files_list",
    detail: "core/internal/agent",
    category: "read",
    status: "done",
    topic: "architecture",
    turnLabel: "Turn #1",
    input: { place: "workspace", path: "core/internal/agent" },
    result: '{"entries": ["http.go", "late.go", "runner.go", "context.go"]}',
  },
  {
    id: "sample-3",
    tool: "desk_open",
    summary: "Desk action · open",
    detail: "surface.workflows",
    category: "desk",
    status: "done",
    topic: "architecture",
    turnLabel: "Turn #1",
    input: { view: "surface.workflows", args: { tab: "architecture" } },
    result: '{"window": "surface-workflows-1", "focused": true}',
  },
  {
    id: "sample-4",
    tool: "card_ask",
    summary: "Asked via choice card",
    detail: "Approve rollout of non-blocking Card Hold wire?",
    category: "card",
    status: "done",
    topic: "cards-ux",
    turnLabel: "Turn #2",
    input: {
      kind: "choice",
      title: "Approve rollout of non-blocking Card Hold wire?",
      hold: true,
      surface: "window",
    },
    result: '{"id": "c_09f2a", "status": "submitted", "data": {"choice": "ship-canary"}}',
  },
  {
    id: "sample-5",
    tool: "notes_write",
    summary: "Executed · notes_write",
    detail: "docs/taste.md",
    category: "write",
    status: "done",
    topic: "theme-polish",
    turnLabel: "Turn #2",
    input: { path: "docs/taste.md", rev: "r42" },
    result: '{"saved": true, "bytes": 6840}',
  },
]

function categoryStyle(cat: ActivityCategory, status: TimelineEntry["status"]) {
  if (status === "error") {
    return {
      dot: "border-destructive/50 bg-destructive/15 text-destructive",
      badge: "border-destructive/30 bg-destructive/15 text-destructive",
      icon: "triangle-alert",
    }
  }
  switch (cat) {
    case "read":
      return {
        dot: "border-chart-2/50 bg-chart-2/15 text-chart-2",
        badge: "border-chart-2/30 bg-chart-2/10 text-chart-2",
        icon: "search",
      }
    case "write":
      return {
        dot: "border-primary/50 bg-primary/15 text-primary",
        badge: "border-primary/30 bg-primary/15 text-primary",
        icon: "terminal",
      }
    case "card":
      return {
        dot: "border-chart-3/50 bg-chart-3/15 text-chart-3",
        badge: "border-chart-3/30 bg-chart-3/15 text-chart-3",
        icon: "gallery-vertical-end",
      }
    case "desk":
      return {
        dot: "border-success/50 bg-success/15 text-success",
        badge: "border-success/30 bg-success/15 text-success",
        icon: "layout-dashboard",
      }
    default:
      return {
        dot: "border-border bg-secondary text-foreground",
        badge: "border-border bg-secondary text-muted-foreground",
        icon: "cpu",
      }
  }
}

export function ActivityTimelineWindow({ win, desk }: ViewProps) {
  const store = useMemo(agentSession, [])
  const ids = useChat(store, (s) => s.ids)
  const items = useChat(store, (s) => s.items)
  const focusGroup = typeof win.args?.group_id === "string" ? win.args.group_id : ""

  const [filter, setFilter] = useState<"all" | ActivityCategory | "error">("all")
  const [query, setQuery] = useState("")
  const [expanded, setExpanded] = useState<Record<string, boolean>>(() => (focusGroup ? { [focusGroup]: true } : {}))
  const [showSample, setShowSample] = useState(false)

  const liveEntries = useMemo<TimelineEntry[]>(() => {
    const out: TimelineEntry[] = []
    let turnIndex = 0
    let currentTopic = "general"
    for (const id of ids) {
      const it: ChatItem | undefined = items[id]
      if (!it) continue
      if (it.kind === "user") {
        turnIndex++
        currentTopic = inferTopicFromText(it.text || it.auto || "", "general")
      } else if (it.kind === "tool") {
        const t = it as ToolItem
        const desc = describeToolCall(t.name, t.input)
        out.push({
          id: t.id,
          tool: t.name,
          summary: desc.summary,
          detail: desc.detail,
          category: desc.category,
          status: t.status,
          topic: currentTopic,
          turnLabel: turnIndex > 0 ? `Turn #${turnIndex}` : "Session",
          input: t.input,
          result: t.result,
        })
      }
    }
    return out
  }, [ids, items])

  const entries = liveEntries.length > 0 && !showSample ? liveEntries : SAMPLE_ENTRIES
  const isUsingSample = liveEntries.length === 0 || showSample

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return entries.filter((e) => {
      if (filter === "error" && e.status !== "error") return false
      if (filter !== "all" && filter !== "error" && e.category !== filter) return false
      if (!q) return true
      return (
        e.tool.toLowerCase().includes(q) ||
        e.summary.toLowerCase().includes(q) ||
        (e.detail?.toLowerCase().includes(q) ?? false) ||
        e.topic.toLowerCase().includes(q)
      )
    })
  }, [entries, filter, query])

  const counts = useMemo(() => {
    return {
      total: entries.length,
      running: entries.filter((e) => e.status === "running").length,
      errors: entries.filter((e) => e.status === "error").length,
    }
  }, [entries])

  const toggle = (id: string) => setExpanded((prev) => ({ ...prev, [id]: !prev[id] }))

  return (
    <div className="flex h-full min-h-0 flex-col bg-background text-xs text-foreground">
      {/* Compact Module Header */}
      <header className="flex flex-wrap items-center gap-1.5 border-b border-border bg-card/60 px-2.5 py-1.5">
        <div className="flex items-center gap-1.5">
          <Icon name="activity" size={14} className="text-primary" />
          <span className="font-mono font-semibold text-foreground">Activity Timeline</span>
          <Badge variant="secondary" className="font-mono">
            {counts.total} {counts.total === 1 ? "step" : "steps"}
          </Badge>
          {counts.running > 0 && (
            <Badge className="border-primary/40 bg-primary/15 font-mono text-primary">{counts.running} active</Badge>
          )}
          {counts.errors > 0 && (
            <Badge variant="destructive" className="font-mono">
              {counts.errors} failed
            </Badge>
          )}
        </div>

        <div className="ml-auto flex flex-wrap items-center gap-1">
          {(
            [
              { id: "all", label: "All" },
              { id: "read", label: "Read / Search" },
              { id: "write", label: "Execute / Write" },
              { id: "card", label: "Cards" },
              { id: "desk", label: "Desk" },
              { id: "error", label: "Errors" },
            ] as const
          ).map((tab) => (
            <Button
              key={tab.id}
              variant={filter === tab.id ? "secondary" : "ghost"}
              size="xs"
              onClick={() => setFilter(tab.id)}
            >
              {tab.label}
            </Button>
          ))}
        </div>
      </header>

      {/* Secondary Filter & Sample Switch Bar */}
      <div className="flex items-center gap-2 border-b border-border/70 bg-card/30 px-2.5 py-1.5">
        <Input
          className="h-6 max-w-60 text-xs"
          placeholder="Filter tool, path, or #topic…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <span className="flex-1 text-[11px] text-muted-foreground">
          {isUsingSample
            ? "Showing golden sample timeline (live tool calls appear here automatically as the agent works)."
            : `Showing ${filtered.length} of ${entries.length} live steps from this conversation.`}
        </span>
        {liveEntries.length > 0 && (
          <Button variant="ghost" size="xs" onClick={() => setShowSample((s) => !s)}>
            {showSample ? "Show live session" : "Preview sample"}
          </Button>
        )}
        <Button
          variant="outline"
          size="xs"
          onClick={() => void desk.open("surface.workflows")}
          title="Open Workflow & Golden Samples Explorer"
        >
          <Icon name="sparkles" size={12} className="text-primary" />
          Workflows
        </Button>
      </div>

      {/* Vertical Activity Timeline */}
      <div className="min-h-0 flex-1 overflow-y-auto px-3.5 py-3">
        <div className="relative flex flex-col gap-2.5 before:absolute before:top-3 before:bottom-3 before:left-[13px] before:w-px before:bg-border">
          {filtered.map((entry) => {
            const st = categoryStyle(entry.category, entry.status)
            const isOpen = Boolean(expanded[entry.id] || entry.id === focusGroup)
            return (
              <div key={entry.id} className="relative pl-8">
                {/* Semantic Timeline Node Icon */}
                <span
                  className={`absolute top-1.5 left-0 flex size-7 items-center justify-center rounded-sm border ${st.dot} ${
                    entry.status === "running" ? "animate-pulse" : ""
                  }`}
                >
                  <Icon name={st.icon} size={13} />
                </span>

                {/* Step Card */}
                <div
                  className={`rounded-md border transition-colors ${
                    isOpen ? "border-primary/40 bg-card" : "border-border/80 bg-card/60 hover:border-border"
                  }`}
                >
                  <Button
                    variant="ghost"
                    className="flex h-auto w-full items-center justify-start gap-2 rounded-none px-3 py-2 text-left font-normal"
                    onClick={() => toggle(entry.id)}
                  >
                    <span className="font-mono text-[11px] font-semibold text-foreground">{entry.summary}</span>
                    <Badge variant="outline" className={`font-mono ${st.badge}`}>
                      {entry.tool}
                    </Badge>
                    {entry.detail && (
                      <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted-foreground">
                        {entry.detail}
                      </span>
                    )}
                    {!entry.detail && <span className="flex-1" />}
                    <Badge
                      variant="outline"
                      className="border-chart-2/30 bg-chart-2/10 font-mono text-[10px] text-chart-2"
                    >
                      #{entry.topic}
                    </Badge>
                    <span className="font-mono text-[10.5px] text-muted-foreground">{entry.turnLabel}</span>
                    <span
                      className={`size-2 rounded-full ${
                        entry.status === "running"
                          ? "animate-ping bg-primary"
                          : entry.status === "error"
                            ? "bg-destructive"
                            : "bg-success"
                      }`}
                      title={entry.status}
                    />
                    <Icon name={isOpen ? "chevron-up" : "chevron-down"} size={13} className="text-muted-foreground" />
                  </Button>

                  {isOpen && (
                    <div className="flex flex-col gap-2 border-t border-border/70 bg-desk/60 px-3 py-2.5">
                      {entry.input !== undefined && (
                        <div className="flex flex-col gap-1">
                          <span className="font-mono text-[10px] tracking-wider text-muted-foreground uppercase">
                            Input Parameters
                          </span>
                          <pre className="m-0 max-h-44 overflow-auto rounded-sm border border-border/70 bg-background/80 p-2 font-mono text-[11px] text-chart-2">
                            {typeof entry.input === "string" ? entry.input : JSON.stringify(entry.input, null, 2)}
                          </pre>
                        </div>
                      )}
                      {entry.result !== undefined && (
                        <div className="flex flex-col gap-1">
                          <span className="font-mono text-[10px] tracking-wider text-muted-foreground uppercase">
                            {entry.status === "error" ? "Error Output" : "Result Payload"}
                          </span>
                          <pre
                            className={`m-0 max-h-52 overflow-auto rounded-sm border p-2 font-mono text-[11px] whitespace-pre-wrap ${
                              entry.status === "error"
                                ? "border-destructive/40 bg-destructive/10 text-destructive"
                                : "border-border/70 bg-background/80 text-foreground"
                            }`}
                          >
                            {entry.result || "(empty)"}
                          </pre>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}
