/**
 * Golden Workflow Samples (`surface.workflows` / `surface/workflows` & Gallery `Workflows` tab):
 * Interactive reference implementations for Surface's core design paradigm:
 *   1. Architecture & Topic Explorer ("Show, Don't Tell" — Current vs. Proposed visual diff + stage pipeline)
 *   2. Goal-Driven Process ("GitHub Repo & Prior Art Review" — deliberate session queue + evidence synthesis + decision bar)
 *   3. Operations Pipeline ("Fleet Registry Update & Canary Deploy" — staged execution + live verification matrix)
 */
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Markdown } from "@/components/ui/markdown"
import { Icon } from "../sdk"
import { Section, Specimen } from "./Specimen"

type WorkflowArchetype = "architecture" | "repo-review" | "fleet-ops"

interface ArchNode {
  id: string
  title: string
  layer: string
  summary: string
  delta?: "unchanged" | "modified" | "added" | "removed"
  changeNote?: string
  metrics?: string
}

const CURRENT_ARCH: ArchNode[] = [
  {
    id: "ctx-strip",
    title: "Inline ContextStrip",
    layer: "Agent Panel Header",
    summary: "Always-visible 4-row banner above chat showing slot, profile, top tools, and raw <context> block.",
    delta: "removed",
    changeNote: "Replaced by compact header icons + persistent ContextModal & Developer AgentInspectorModal.",
    metrics: "148px vertical height",
  },
  {
    id: "card-ask-block",
    title: "Blocking card_ask Wire",
    layer: "Kernel / Agent Turn",
    summary: "Blocks chat composer until answered inline; single question per call.",
    delta: "modified",
    changeNote: "Added POST /api/cards/{id}/hold, questions[] batching, wizard pagination, and submitLateBatch.",
    metrics: "1 card / turn (blocking)",
  },
  {
    id: "tool-rows",
    title: "Unfolded Tool Rows",
    layer: "Conversation Stream",
    summary: "Every tool call renders its own collapsible box inline in the chat thread.",
    delta: "modified",
    changeNote: "Consecutive tool calls collapse into a single ActivityFoldRow opening the ActivityTimeline window.",
    metrics: "N boxes / turn",
  },
]

const PROPOSED_ARCH: ArchNode[] = [
  {
    id: "compact-header",
    title: "Compact ChatHeader + Modals",
    layer: "Agent Panel Header",
    summary: "Single 32px header with #slot Context icon, Cards badge, Activity button, and gated Dev Inspector.",
    delta: "added",
    changeNote: "Saves 116px vertical height; modal states persist in localStorage; live GUI dev mode toggle.",
    metrics: "32px vertical height (-78%)",
  },
  {
    id: "card-hold-wizard",
    title: "Non-Blocking Hold & Wizard Batch",
    layer: "Kernel + surface.cards Window",
    summary: "One-click Hold frees chat immediately; Cards window paginates N-at-a-time and submits in one batch turn.",
    delta: "modified",
    changeNote: "Universal 'Other: tell the agent…' on all cards; batch answers consolidated in late.go.",
    metrics: "N cards / wizard step",
  },
  {
    id: "activity-timeline",
    title: "Single-Line Fold + Activity Timeline",
    layer: "Conversation + surface.activity",
    summary: "1-line live status bar in chat; clicking opens vertical Activity Timeline with semantic step drilldown.",
    delta: "added",
    changeNote: "Topic/thread #tags on every turn; semantic cyan/amber/magenta/emerald step nodes.",
    metrics: "1 line / turn group",
  },
]

const PIPELINE_STEPS = [
  {
    num: "01",
    title: "Context & Inspector Extraction",
    status: "done" as const,
    owner: "AgentPanel.tsx",
    points: ["Single-row header chrome", "5-tab Developer Inspector", "Classifier feedback via CardChoice"],
  },
  {
    num: "02",
    title: "Card Hold & Wizard Pagination",
    status: "done" as const,
    owner: "builtin.go · late.go",
    points: ["POST /api/cards/{id}/hold", "N-at-a-time wizard step bar", "Universal 'Other: tell agent' option"],
  },
  {
    num: "03",
    title: "Activity Fold & Timeline Window",
    status: "done" as const,
    owner: "ActivityTimeline.tsx",
    points: ["1-line tool chain fold", "Semantic timeline spine", "Expandable JSON payload drilldown"],
  },
  {
    num: "04",
    title: "Workflow Process Surfaces",
    status: "active" as const,
    owner: "workflows.tsx · taste.md",
    points: ["Visual Current vs. Proposed diff", "Goal-driven Repo Review session", "Staged Fleet Rollout gate"],
  },
]

interface RepoCandidate {
  id: string
  repo: string
  tagline: string
  stars: string
  license: string
  goalFit: "High Fit" | "Partial Fit" | "Reference Only"
  novelPattern: string
  ourStackComparison: { aspect: string; candidate: string; surface: string; verdict: "Adopt" | "Adapt" | "Skip" }[]
  recommendation: string
  status: "pending" | "landed" | "skipped" | "held"
}

const INITIAL_REPOS: RepoCandidate[] = [
  {
    id: "repo-1",
    repo: "assistant-ui/assistant-ui",
    tagline: "Composable primitives for AI chat, tool UIs, and human-in-the-loop streams",
    stars: "4.8k",
    license: "MIT",
    goalFit: "High Fit",
    novelPattern: "Stream-part grouping & declarative tool-call state machines with interrupt/resume handles.",
    ourStackComparison: [
      {
        aspect: "Tool Call Folding",
        candidate: "ThreadPrimitive.Messages groups consecutive tool parts into a single virtualized slot",
        surface: "ActivityFoldRow in AgentPanel.tsx + dedicated surface.activity timeline window",
        verdict: "Adopt",
      },
      {
        aspect: "Human Approval Interrupts",
        candidate: "Inline tool UI only; blocks thread until resolved",
        surface: "CardDock + POST /api/cards/{id}/hold non-blocking promotion to surface.cards wizard",
        verdict: "Skip",
      },
      {
        aspect: "Markdown Streaming",
        candidate: "Block-memoized GFM with auto-closed half-tokens",
        surface: "splitBlocks() + closeOpen() in registry/ui/markdown.tsx",
        verdict: "Adapt",
      },
    ],
    recommendation:
      "Land finding to Tesseract (`surface/prior-art`) for virtualized sub-agent step streams; keep our non-blocking Card Hold architecture.",
    status: "pending",
  },
  {
    id: "repo-2",
    repo: "mendableai/open-lovable",
    tagline: "Sandbox-backed generative UI workbench with live AST diff preview",
    stars: "3.1k",
    license: "Apache-2.0",
    goalFit: "Partial Fit",
    novelPattern: "Side-by-side visual architecture delta view before applying multi-file component patches.",
    ourStackComparison: [
      {
        aspect: "Architecture Diffing",
        candidate: "Visual node graph comparing current vs. proposed module topology",
        surface: "Workflow Architecture Explorer ('Show, Don't Tell' diff blocks)",
        verdict: "Adopt",
      },
      {
        aspect: "Preview Runtime",
        candidate: "Remote cloud container per session",
        surface: "Local Vite gallery + Go embedded desk views",
        verdict: "Skip",
      },
    ],
    recommendation: "Adopt the visual before/after node delta card pattern for architecture exploration turns.",
    status: "pending",
  },
  {
    id: "repo-3",
    repo: "basecamp/omarchy",
    tagline: "Opinionated Arch + Hyprland rice configuration and terminal theme system",
    stars: "6.4k",
    license: "MIT",
    goalFit: "High Fit",
    novelPattern: "13-token palette contract driving window borders, active focus gradients, and mono status bars.",
    ourStackComparison: [
      {
        aspect: "Window Chrome",
        candidate: "2px gradient active border + compact right-aligned window controls",
        surface: "flexlayout tabset gradient border-box + right-aligned [Float, Max, X] toolbar",
        verdict: "Adopt",
      },
    ],
    recommendation:
      "Already integrated as the foundation of @hollis-labs/surface-theme; lock Evolved Blade Runner as default.",
    status: "landed",
  },
]

function ArchitectureExplorerMock() {
  const [viewMode, setViewMode] = useState<"split" | "proposed" | "current">("split")
  const [selectedNode, setSelectedNode] = useState<ArchNode>(PROPOSED_ARCH[1])

  const badgeForDelta = (delta?: ArchNode["delta"]) => {
    switch (delta) {
      case "added":
        return <Badge className="border-success/40 bg-success/15 font-mono text-success">+ ADDED</Badge>
      case "modified":
        return <Badge className="border-warning/40 bg-warning/15 font-mono text-warning">~ MODIFIED</Badge>
      case "removed":
        return (
          <Badge variant="destructive" className="font-mono">
            - REMOVED
          </Badge>
        )
      default:
        return (
          <Badge variant="secondary" className="font-mono">
            UNCHANGED
          </Badge>
        )
    }
  }

  return (
    <div className="flex flex-col gap-3 rounded-md border border-border bg-background p-3.5 text-xs">
      {/* Top Executive Summary Strip (No Wall of Text) */}
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border/80 pb-3">
        <div className="flex max-w-2xl flex-col gap-1">
          <div className="flex items-center gap-2">
            <Badge variant="outline" className="border-chart-2/40 bg-chart-2/10 font-mono text-chart-2">
              ARCH DIFF · CW-20261120-0060
            </Badge>
            <span className="font-mono text-[11px] text-muted-foreground">Surface Desk · Agent & Card Interaction</span>
          </div>
          <h3 className="font-mono text-sm font-semibold text-foreground">
            Compact Context Chrome, Non-Blocking Card Hold & Single-Line Activity Folding
          </h3>
          <p className="text-xs text-muted-foreground">
            Reclaims 78% of agent panel header space, decouples multi-question cards from blocking chat turns, and
            collapses tool noise into a 1-line drilldown trigger.
          </p>
        </div>

        {/* KPI Delta Strip */}
        <div className="flex flex-wrap items-center gap-2">
          <div className="rounded-sm border border-border/80 bg-card px-2.5 py-1.5">
            <div className="font-mono text-[10px] text-muted-foreground uppercase">Header Chrome</div>
            <div className="font-mono text-sm font-semibold text-success">148px → 32px</div>
          </div>
          <div className="rounded-sm border border-border/80 bg-card px-2.5 py-1.5">
            <div className="font-mono text-[10px] text-muted-foreground uppercase">Chat Blocking</div>
            <div className="font-mono text-sm font-semibold text-primary">1-Click Hold</div>
          </div>
          <div className="rounded-sm border border-border/80 bg-card px-2.5 py-1.5">
            <div className="font-mono text-[10px] text-muted-foreground uppercase">Tool Stream</div>
            <div className="font-mono text-sm font-semibold text-chart-2">1-Line Fold</div>
          </div>
        </div>
      </div>

      {/* Mode Switcher */}
      <div className="flex items-center justify-between gap-2">
        <span className="font-mono text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">
          Visual Architecture Delta (Click any node to inspect change contract)
        </span>
        <div className="flex items-center gap-1">
          {(
            [
              { id: "split", label: "Side-by-Side Diff" },
              { id: "current", label: "Before (Current)" },
              { id: "proposed", label: "After (Proposed)" },
            ] as const
          ).map((m) => (
            <Button
              key={m.id}
              variant={viewMode === m.id ? "secondary" : "ghost"}
              size="xs"
              onClick={() => setViewMode(m.id)}
            >
              {m.label}
            </Button>
          ))}
        </div>
      </div>

      {/* Visual Architecture Comparison Grid */}
      <div className={`grid gap-3 ${viewMode === "split" ? "grid-cols-1 @2xl:grid-cols-2" : "grid-cols-1"}`}>
        {(viewMode === "split" || viewMode === "current") && (
          <div className="flex flex-col gap-2 rounded-md border border-border/80 bg-desk/50 p-3">
            <div className="flex items-center justify-between border-b border-border/60 pb-1.5">
              <span className="font-mono text-[11px] font-semibold text-muted-foreground uppercase">
                Current Architecture (Before)
              </span>
              <Badge variant="outline" className="font-mono text-muted-foreground">
                3 bottlenecks
              </Badge>
            </div>
            {CURRENT_ARCH.map((node) => (
              <Button
                key={node.id}
                variant="ghost"
                onClick={() => setSelectedNode(node)}
                className={`flex h-auto flex-col items-stretch gap-1 rounded-sm border p-2.5 text-left font-normal whitespace-normal transition-colors ${
                  selectedNode.id === node.id
                    ? "border-primary bg-card"
                    : "border-border/70 bg-card/50 hover:border-border"
                }`}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="font-mono text-xs font-semibold text-foreground">{node.title}</span>
                  {badgeForDelta(node.delta)}
                </div>
                <div className="font-mono text-[10.5px] text-chart-2">{node.layer}</div>
                <p className="text-[11.5px] text-muted-foreground">{node.summary}</p>
                {node.metrics && (
                  <div className="mt-1 font-mono text-[10.5px] text-destructive">Footprint: {node.metrics}</div>
                )}
              </Button>
            ))}
          </div>
        )}

        {(viewMode === "split" || viewMode === "proposed") && (
          <div className="flex flex-col gap-2 rounded-md border border-primary/35 bg-desk/50 p-3">
            <div className="flex items-center justify-between border-b border-primary/25 pb-1.5">
              <span className="font-mono text-[11px] font-semibold text-primary uppercase">
                Proposed Architecture (What Changes)
              </span>
              <Badge className="border-success/40 bg-success/15 font-mono text-success">2 added · 1 upgraded</Badge>
            </div>
            {PROPOSED_ARCH.map((node) => (
              <Button
                key={node.id}
                variant="ghost"
                onClick={() => setSelectedNode(node)}
                className={`flex h-auto flex-col items-stretch gap-1 rounded-sm border p-2.5 text-left font-normal whitespace-normal transition-colors ${
                  selectedNode.id === node.id
                    ? "border-primary bg-card"
                    : node.delta === "added"
                      ? "border-success/35 bg-success/5 hover:border-success/60"
                      : "border-warning/35 bg-warning/5 hover:border-warning/60"
                }`}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="font-mono text-xs font-semibold text-foreground">{node.title}</span>
                  {badgeForDelta(node.delta)}
                </div>
                <div className="font-mono text-[10.5px] text-chart-2">{node.layer}</div>
                <p className="text-[11.5px] text-foreground/90">{node.summary}</p>
                {node.changeNote && (
                  <div className="mt-1 rounded-xs border-l-2 border-primary bg-accent/25 px-2 py-1 text-[11px] text-foreground">
                    <strong className="font-mono text-primary">Delta:</strong> {node.changeNote}
                  </div>
                )}
                {node.metrics && (
                  <div className="mt-0.5 font-mono text-[10.5px] text-success">Impact: {node.metrics}</div>
                )}
              </Button>
            ))}
          </div>
        )}
      </div>

      {/* Numbered Stage Pipeline (Inspired by ~/Downloads/surface-refs/workflow-process/*) */}
      <div className="flex flex-col gap-2 pt-1">
        <span className="font-mono text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">
          Execution Pipeline
        </span>
        <div className="grid grid-cols-1 gap-2 @xl:grid-cols-4">
          {PIPELINE_STEPS.map((step) => (
            <div
              key={step.num}
              className={`flex flex-col gap-1.5 rounded-sm border p-2.5 ${
                step.status === "active" ? "border-primary/60 bg-accent/20" : "border-border/80 bg-card/70"
              }`}
            >
              <div className="flex items-center justify-between">
                <span className="font-mono text-base font-bold text-primary">{step.num}</span>
                <Badge
                  variant="outline"
                  className={
                    step.status === "done"
                      ? "border-success/40 bg-success/10 font-mono text-success"
                      : "border-primary/40 bg-primary/15 font-mono text-primary"
                  }
                >
                  {step.status === "done" ? "VERIFIED" : "IN PROGRESS"}
                </Badge>
              </div>
              <div className="font-mono text-xs font-semibold text-foreground">{step.title}</div>
              <div className="font-mono text-[10.5px] text-chart-2">{step.owner}</div>
              <ul className="m-0 flex flex-col gap-0.5 pl-3.5 text-[11px] text-muted-foreground">
                {step.points.map((pt) => (
                  <li key={pt} className="marker:text-primary">
                    {pt}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

function RepoReviewWorkflowMock() {
  const [repos, setRepos] = useState<RepoCandidate[]>(INITIAL_REPOS)
  const [activeId, setActiveId] = useState<string>(INITIAL_REPOS[0].id)
  const [customNote, setCustomNote] = useState("")
  const [otherMode, setOtherMode] = useState(false)

  const current = repos.find((r) => r.id === activeId) ?? repos[0]
  const landedCount = repos.filter((r) => r.status === "landed").length
  const reviewedCount = repos.filter((r) => r.status !== "pending").length

  const decide = (status: RepoCandidate["status"]) => {
    setRepos((prev) => prev.map((r) => (r.id === current.id ? { ...r, status } : r)))
    setOtherMode(false)
    setCustomNote("")
    const nextPending = repos.find((r) => r.id !== current.id && r.status === "pending")
    if (nextPending) setActiveId(nextPending.id)
  }

  return (
    <div className="flex flex-col gap-3 rounded-md border border-border bg-background p-3.5 text-xs">
      {/* Deliberate Session Goal Banner */}
      <div className="flex flex-wrap items-center justify-between gap-2 rounded-sm border border-primary/30 bg-accent/20 px-3 py-2">
        <div className="flex items-center gap-2">
          <Icon name="compass" size={15} className="text-primary" />
          <div>
            <div className="font-mono text-[10.5px] text-primary uppercase">
              Active Guided Workflow · Prior Art Triage
            </div>
            <div className="font-mono text-xs font-semibold text-foreground">
              “Work through GitHub repo reviews and land novel UI/agent findings to Tesseract”
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Badge variant="outline" className="border-chart-2/40 bg-chart-2/10 font-mono text-chart-2">
            {reviewedCount} / {repos.length} reviewed
          </Badge>
          <Badge className="border-success/40 bg-success/15 font-mono text-success">{landedCount} landed</Badge>
        </div>
      </div>

      {/* Queue Stepper + Active Synthesis Canvas */}
      <div className="grid grid-cols-1 gap-3 @2xl:grid-cols-[240px_1fr]">
        {/* Left Queue Stepper (Not a passive filter table) */}
        <div className="flex flex-col gap-1.5 rounded-sm border border-border/80 bg-card/50 p-2">
          <div className="px-1 font-mono text-[10.5px] font-semibold text-muted-foreground uppercase">
            Session Queue ({repos.length})
          </div>
          {repos.map((r, idx) => (
            <Button
              key={r.id}
              variant="ghost"
              onClick={() => setActiveId(r.id)}
              className={`flex h-auto flex-col items-stretch gap-1 rounded-sm border p-2 text-left font-normal whitespace-normal transition-colors ${
                r.id === current.id ? "border-primary bg-card" : "border-border/60 bg-background/60 hover:border-border"
              }`}
            >
              <div className="flex items-center justify-between gap-1">
                <span className="font-mono text-[11px] font-semibold text-foreground">
                  0{idx + 1} · {r.repo.split("/")[1]}
                </span>
                <Badge
                  variant="outline"
                  className={
                    r.status === "landed"
                      ? "border-success/40 bg-success/15 font-mono text-[10px] text-success"
                      : r.status === "held"
                        ? "border-warning/40 bg-warning/15 font-mono text-[10px] text-warning"
                        : r.status === "skipped"
                          ? "font-mono text-[10px] text-muted-foreground"
                          : "border-chart-2/40 bg-chart-2/10 font-mono text-[10px] text-chart-2"
                  }
                >
                  {r.status}
                </Badge>
              </div>
              <span className="truncate text-[11px] text-muted-foreground">{r.tagline}</span>
            </Button>
          ))}
        </div>

        {/* Focused Decision & Evidence Canvas */}
        <div className="flex flex-col gap-3 rounded-sm border border-border/80 bg-card/70 p-3">
          <div className="flex flex-wrap items-start justify-between gap-2 border-b border-border/70 pb-2">
            <div>
              <div className="flex items-center gap-2">
                <span className="font-mono text-sm font-bold text-primary">{current.repo}</span>
                <Badge variant="secondary" className="font-mono">
                  ★ {current.stars}
                </Badge>
                <Badge variant="outline" className="font-mono">
                  {current.license}
                </Badge>
              </div>
              <p className="mt-0.5 text-xs text-muted-foreground">{current.tagline}</p>
            </div>
            <Badge
              className={
                current.goalFit === "High Fit"
                  ? "border-success/40 bg-success/15 font-mono text-success"
                  : "border-warning/40 bg-warning/15 font-mono text-warning"
              }
            >
              {current.goalFit}
            </Badge>
          </div>

          {/* Novel Pattern Callout */}
          <div className="rounded-sm border-l-2 border-chart-2 bg-desk/70 px-3 py-2">
            <div className="font-mono text-[10.5px] font-semibold text-chart-2 uppercase">
              Extracted Novel Pattern / Prior Art
            </div>
            <p className="mt-0.5 text-xs text-foreground">{current.novelPattern}</p>
          </div>

          {/* Structured Architecture Comparison Table */}
          <div className="overflow-x-auto rounded-sm border border-border">
            <table className="w-full border-collapse text-xs">
              <thead className="border-b border-border bg-muted/70">
                <tr>
                  <th className="px-2.5 py-1.5 text-left font-mono text-[10.5px] text-muted-foreground uppercase">
                    Dimension
                  </th>
                  <th className="px-2.5 py-1.5 text-left font-mono text-[10.5px] text-muted-foreground uppercase">
                    {current.repo.split("/")[1]} Approach
                  </th>
                  <th className="px-2.5 py-1.5 text-left font-mono text-[10.5px] text-muted-foreground uppercase">
                    Surface Desk Architecture
                  </th>
                  <th className="px-2.5 py-1.5 text-right font-mono text-[10.5px] text-muted-foreground uppercase">
                    Verdict
                  </th>
                </tr>
              </thead>
              <tbody>
                {current.ourStackComparison.map((row) => (
                  <tr key={row.aspect} className="border-t border-border/60">
                    <td className="px-2.5 py-1.5 font-mono font-medium text-foreground">{row.aspect}</td>
                    <td className="px-2.5 py-1.5 text-muted-foreground">{row.candidate}</td>
                    <td className="px-2.5 py-1.5 text-foreground/90">{row.surface}</td>
                    <td className="px-2.5 py-1.5 text-right">
                      <Badge
                        variant="outline"
                        className={
                          row.verdict === "Adopt"
                            ? "border-success/40 bg-success/15 font-mono text-success"
                            : row.verdict === "Adapt"
                              ? "border-primary/40 bg-primary/15 font-mono text-primary"
                              : "font-mono text-muted-foreground"
                        }
                      >
                        {row.verdict}
                      </Badge>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Agent Synthesis & Action Bar */}
          <div className="flex flex-col gap-2 rounded-sm border border-border/80 bg-desk/60 p-2.5">
            <div className="flex items-center gap-1.5">
              <span className="size-1.5 rounded-full bg-primary" />
              <span className="font-mono text-[11px] font-semibold text-primary">Agent Recommendation</span>
            </div>
            <p className="text-xs text-foreground/90">{current.recommendation}</p>

            {otherMode && (
              <div className="flex items-center gap-2 pt-1">
                <Input
                  className="h-6.5 flex-1 text-xs"
                  placeholder="Tell the agent how to handle this repo finding…"
                  value={customNote}
                  onChange={(e) => setCustomNote(e.target.value)}
                />
                <Button size="xs" onClick={() => decide("landed")} disabled={!customNote.trim()}>
                  Send instruction & land
                </Button>
              </div>
            )}

            <div className="flex flex-wrap items-center gap-1.5 pt-1">
              <Button size="xs" onClick={() => decide("landed")}>
                <Icon name="check" size={12} />
                Land Finding to Tesseract & File Follow-up
              </Button>
              <Button variant="secondary" size="xs" onClick={() => decide("held")}>
                <Icon name="pin" size={12} />
                Hold in Cards Drawer
              </Button>
              <Button variant="outline" size="xs" onClick={() => decide("skipped")}>
                Skip Repo
              </Button>
              <Button variant="ghost" size="xs" onClick={() => setOtherMode((m) => !m)}>
                Other: tell the agent…
              </Button>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

function FleetOperationsWorkflowMock() {
  const [approved, setApproved] = useState(false)
  return (
    <div className="flex flex-col gap-3 rounded-md border border-border bg-background p-3.5 text-xs">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border pb-2.5">
        <div>
          <div className="flex items-center gap-2">
            <Badge className="border-primary/40 bg-primary/15 font-mono text-primary">
              FLEET OPS · REGISTRY ROLLOUT
            </Badge>
            <span className="font-mono text-[11px] text-muted-foreground">ops-host tailscale mesh · 4 nodes</span>
          </div>
          <h3 className="mt-1 font-mono text-sm font-semibold text-foreground">
            Publish @surface/ui Registry v0.4.1 & Roll Out Evolved Blade Runner Theme
          </h3>
        </div>
        <Badge
          variant="outline"
          className={
            approved
              ? "border-success/40 bg-success/15 font-mono text-success"
              : "border-warning/40 bg-warning/15 font-mono text-warning"
          }
        >
          {approved ? "ROLLOUT COMPLETE" : "AWAITING CANARY GATE"}
        </Badge>
      </div>

      <div className="grid grid-cols-1 gap-3 @xl:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <span className="font-mono text-[10.5px] font-semibold text-muted-foreground uppercase">
            Registry & Config Delta
          </span>
          <Markdown>{`\`\`\`diff
@@ registry/theme/surface.css @@
- --radius-4xl: calc(var(--radius) * 2.6);
+ --radius-sm: 4px;
+ --radius-md: 5px;
+ --p-bg: #0c1215;
+ --p-accent: #e5a463;
+ --p-accent-2: #7ecfd4;
\`\`\``}</Markdown>
        </div>

        <div className="flex flex-col gap-2">
          <span className="font-mono text-[10.5px] font-semibold text-muted-foreground uppercase">
            Fleet Target Nodes
          </span>
          <div className="flex flex-col gap-1.5">
            {[
              { host: "surface-desk.local (127.0.0.1:8190)", role: "Canary · Local ADE", state: "verified" },
              {
                host: "ops-host-core.tailnet:8190",
                role: "Primary Agent OS Host",
                state: approved ? "verified" : "staged",
              },
              {
                host: "studio-node.tailnet:8195",
                role: "Design & Eval Studio",
                state: approved ? "verified" : "staged",
              },
            ].map((n) => (
              <div
                key={n.host}
                className="flex items-center justify-between rounded-sm border border-border/80 bg-card/70 px-2.5 py-1.5"
              >
                <div>
                  <div className="font-mono text-xs font-semibold text-foreground">{n.host}</div>
                  <div className="text-[11px] text-muted-foreground">{n.role}</div>
                </div>
                <Badge
                  variant="outline"
                  className={
                    n.state === "verified"
                      ? "border-success/40 bg-success/15 font-mono text-success"
                      : "border-chart-2/40 bg-chart-2/10 font-mono text-chart-2"
                  }
                >
                  {n.state}
                </Badge>
              </div>
            ))}
          </div>

          <div className="mt-auto flex items-center gap-2 rounded-sm border border-primary/30 bg-accent/20 p-2.5">
            <span className="flex-1 text-xs text-foreground">
              {approved
                ? "All 3 fleet nodes synced and verified against registry checksum."
                : "Canary verified on surface-desk.local. Promote to tailnet nodes?"}
            </span>
            <Button size="xs" onClick={() => setApproved((a) => !a)}>
              {approved ? "Reset Mock" : "Approve Fleet Rollout"}
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}

export function Workflows() {
  const [active, setActive] = useState<WorkflowArchetype>("architecture")

  return (
    <div className="flex flex-col gap-5 p-3">
      <Section
        title="Workflow & Process Golden Samples"
        about="Surface prioritizes behavior, guided workflows, and visual 'Show, Don't Tell' deltas over passive CRUD tables or walls of text. Use these golden samples to align on interaction patterns."
      >
        <Specimen label="Interactive Workflow Switcher · Pick a workflow archetype to explore" wide>
          <div className="flex w-full flex-col gap-3">
            <div className="flex flex-wrap items-center gap-1.5 border-b border-border pb-2">
              <Button
                variant={active === "architecture" ? "default" : "outline"}
                size="sm"
                onClick={() => setActive("architecture")}
              >
                <Icon name="git-compare-arrows" size={13} />
                01 · Architecture & Topic Explorer (“Show, Don't Tell”)
              </Button>
              <Button
                variant={active === "repo-review" ? "default" : "outline"}
                size="sm"
                onClick={() => setActive("repo-review")}
              >
                <Icon name="compass" size={13} />
                02 · Guided Process (“GitHub Repo & Prior Art Review”)
              </Button>
              <Button
                variant={active === "fleet-ops" ? "default" : "outline"}
                size="sm"
                onClick={() => setActive("fleet-ops")}
              >
                <Icon name="server" size={13} />
                03 · Operations Pipeline (“Registry Update & Fleet Deploy”)
              </Button>
            </div>

            {active === "architecture" && <ArchitectureExplorerMock />}
            {active === "repo-review" && <RepoReviewWorkflowMock />}
            {active === "fleet-ops" && <FleetOperationsWorkflowMock />}
          </div>
        </Specimen>
      </Section>
    </div>
  )
}
