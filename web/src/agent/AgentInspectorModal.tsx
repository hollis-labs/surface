/**
 * The Agent Inspector modal (gated by surface.developer_mode):
 * - Context & Pressure: active slot/profile, byte & char usage vs limit, trimmed items, Around lines, <context> block
 * - Usage & Runs: token usage (uncached, cache read, cache write, output, cache hit ratio), duration, tool calls
 * - Memory & Notes: standing instructions (context/standing.md), per-slot notes, standing permission grants
 * - Capabilities & Steering: active profile skills/variants/tools, reflexes, routing rules
 * - Trace & Classifier: turn-by-turn history and shadow classifier replay report
 */
import { useCallback, useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Progress } from "@/components/ui/progress"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { showCard } from "../cards/store"
import { Icon, useEvent } from "../sdk"
import {
  type Assembly,
  agentApi,
  type ProfileState,
  type ReflexInfo,
  type ReplayReport,
  type RuleInfo,
  type RunRecord,
  type Setup,
  type SlotInfo,
  type StandingGrant,
  type StandingState,
  type TraceTurn,
} from "./api"

const when = (iso?: string) => {
  if (!iso) return ""
  const d = new Date(iso)
  return Number.isNaN(d.getTime())
    ? iso
    : d.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit" })
}

export interface AgentInspectorModalProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  asm: Assembly | null
  setup: Setup | null
  slots: SlotInfo[]
  profiles: ProfileState | null
}

export function AgentInspectorModal({ open, onOpenChange, asm, setup, slots, profiles }: AgentInspectorModalProps) {
  const [tab, setTab] = useState("pressure")
  const [runs, setRuns] = useState<RunRecord[]>([])
  const [standing, setStanding] = useState<StandingState | null>(null)
  const [grants, setGrants] = useState<StandingGrant[]>([])
  const [reflexes, setReflexes] = useState<ReflexInfo[]>([])
  const [rules, setRules] = useState<{ owner: RuleInfo[]; builtin: RuleInfo[] }>({ owner: [], builtin: [] })
  const [trace, setTrace] = useState<{ on: boolean; turns: TraceTurn[] }>({ on: true, turns: [] })
  const [replay, setReplay] = useState<ReplayReport | null>(null)

  const load = useCallback(() => {
    if (!open) return
    agentApi.runs(50).then(setRuns, () => {})
    agentApi.standing().then(setStanding, () => {})
    agentApi.grants().then(setGrants, () => {})
    agentApi.reflexes().then(setReflexes, () => {})
    agentApi.rules().then(setRules, () => {})
    agentApi.trace(50).then(setTrace, () => {})
    agentApi.replay().then(setReplay, () => {})
  }, [open])

  useEffect(() => {
    load()
  }, [load])
  useEvent("agent.turn", load)
  useEvent("agent.run.ended", load)
  useEvent("agent.standing", load)
  useEvent("agent.reflexes", load)
  useEvent("agent.note", load)

  const blockBytes = asm ? new TextEncoder().encode(asm.block).length : 0
  const limitBytes = setup?.context_limit_bytes || 7800
  const blockPct = Math.min(100, Math.round((blockBytes / limitBytes) * 100))

  const latestRun = runs[0]
  const latestTotalInput = latestRun ? latestRun.uncached_input + latestRun.cache_read + latestRun.cache_write : 0
  const compactAt = 110_000
  const windowPct = Math.min(100, Math.round((latestTotalInput / compactAt) * 100))

  const totals = runs.reduce(
    (acc, r) => ({
      uncached: acc.uncached + r.uncached_input,
      cacheRead: acc.cacheRead + r.cache_read,
      cacheWrite: acc.cacheWrite + r.cache_write,
      output: acc.output + r.output,
      tools: acc.tools + (r.tools?.length ?? 0),
    }),
    { uncached: 0, cacheRead: 0, cacheWrite: 0, output: 0, tools: 0 },
  )
  const totalInput = totals.uncached + totals.cacheRead + totals.cacheWrite
  const cacheHitPct = totalInput > 0 ? Math.round((totals.cacheRead / totalInput) * 100) : 0

  const askSlotCorrectionCard = (turnText: string, currentSlot: string) => {
    void showCard({
      kind: "choice",
      title: "Classifier feedback: which slot should this turn use?",
      subject: "classify:slot",
      source: "agent",
      payload: {
        question: `Turn: “${turnText.slice(0, 120)}” (chose ${currentSlot || "none"})`,
        text: turnText,
        options: slots.map((s) => ({ id: s.name, label: s.name, description: s.about })),
        allow_other: true,
      },
    })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[85vh] flex-col gap-3 sm:max-w-3xl">
        <DialogHeader>
          <div className="flex items-center justify-between pr-6">
            <DialogTitle className="flex items-center gap-2">
              <Icon name="activity" size={16} />
              Agent Inspector
            </DialogTitle>
            <Button variant="ghost" size="xs" onClick={load} title="Refresh inspector data">
              <Icon name="refresh-cw" size={13} />
              Refresh
            </Button>
          </div>
          <DialogDescription>
            Context pressure, token usage, tool calls, memory, standing notes, skills, reflexes and trace.
          </DialogDescription>
        </DialogHeader>

        <Tabs value={tab} onValueChange={(v) => setTab(String(v))} className="flex min-h-0 flex-1 flex-col">
          <TabsList className="w-full justify-start">
            <TabsTrigger value="pressure">Context &amp; Pressure</TabsTrigger>
            <TabsTrigger value="runs">Usage &amp; Runs ({runs.length})</TabsTrigger>
            <TabsTrigger value="memory">Memory &amp; Notes</TabsTrigger>
            <TabsTrigger value="capabilities">Skills &amp; Reflexes</TabsTrigger>
            <TabsTrigger value="trace">Trace &amp; Classifier</TabsTrigger>
          </TabsList>

          <TabsContent value="pressure" className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto pt-2 text-xs">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="flex flex-col gap-1.5 rounded-lg border p-3">
                <div className="flex items-center justify-between">
                  <span className="font-medium">Turn Context Slot Budget</span>
                  <span className="font-mono text-muted-foreground">
                    {blockBytes.toLocaleString()} / {limitBytes.toLocaleString()} B ({blockPct}%)
                  </span>
                </div>
                <Progress value={blockPct} />
                <span className="text-muted-foreground">
                  {asm?.trimmed && asm.trimmed.length > 0
                    ? `Trimmed: ${asm.trimmed.join(", ")}`
                    : "Nothing trimmed by budget."}
                </span>
              </div>

              <div className="flex flex-col gap-1.5 rounded-lg border p-3">
                <div className="flex items-center justify-between">
                  <span className="font-medium">Conversation Compaction Pressure</span>
                  <span className="font-mono text-muted-foreground">
                    {latestTotalInput.toLocaleString()} / {compactAt.toLocaleString()} tok ({windowPct}%)
                  </span>
                </div>
                <Progress value={windowPct} />
                <span className="text-muted-foreground">
                  {latestRun
                    ? `Last run (${latestRun.run_id}): ${latestRun.cache_read.toLocaleString()} cache read, ${latestRun.cache_write.toLocaleString()} write`
                    : "No runs recorded yet."}
                </span>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2 rounded-lg border p-2.5">
              <span className="text-muted-foreground">Active slot:</span>
              <Badge>{asm?.slot || "none"}</Badge>
              <span className="text-muted-foreground">({asm?.why || "default"})</span>
              {asm?.profile && (
                <>
                  <span className="text-muted-foreground">· Profile:</span>
                  <Badge variant="secondary">{asm.profile}</Badge>
                </>
              )}
              {asm?.focused && (
                <>
                  <span className="text-muted-foreground">· In front:</span>
                  <Badge variant="outline">{asm.focused.view}</Badge>
                  <span className="truncate">{asm.focused.title}</span>
                </>
              )}
            </div>

            {asm?.around && asm.around.length > 0 && (
              <div className="flex flex-col gap-1 rounded-lg border p-2.5">
                <span className="font-medium">Around (carried across all slots)</span>
                {asm.around.map((a) => (
                  <div key={a.module} className="flex gap-2">
                    <Badge variant="outline" className="shrink-0 font-mono">
                      {a.module}
                    </Badge>
                    <span className="text-muted-foreground">{a.text}</span>
                  </div>
                ))}
              </div>
            )}

            <div className="flex flex-col gap-1">
              <span className="font-medium">Assembled &lt;context&gt; block</span>
              <pre className="m-0 max-h-64 overflow-auto rounded-md border bg-muted/40 p-2.5 font-mono text-xs break-words whitespace-pre-wrap">
                {asm?.block || "(empty)"}
              </pre>
            </div>
          </TabsContent>

          <TabsContent value="runs" className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto pt-2 text-xs">
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              <div className="rounded-lg border p-2.5">
                <div className="text-muted-foreground">Total Input (recent)</div>
                <div className="font-mono text-sm font-semibold">{totalInput.toLocaleString()}</div>
              </div>
              <div className="rounded-lg border p-2.5">
                <div className="text-muted-foreground">Cache Hit Ratio</div>
                <div className="font-mono text-sm font-semibold">{cacheHitPct}%</div>
              </div>
              <div className="rounded-lg border p-2.5">
                <div className="text-muted-foreground">Total Output</div>
                <div className="font-mono text-sm font-semibold">{totals.output.toLocaleString()}</div>
              </div>
              <div className="rounded-lg border p-2.5">
                <div className="text-muted-foreground">Tool Calls</div>
                <div className="font-mono text-sm font-semibold">{totals.tools}</div>
              </div>
            </div>

            <div className="flex flex-col gap-1.5">
              {runs.length === 0 && <p className="text-muted-foreground">No runs recorded yet.</p>}
              {runs.map((r) => (
                <div key={r.run_id} className="flex flex-col gap-1 rounded-md border p-2.5">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <Badge
                      variant={
                        r.status === "finished" ? "secondary" : r.status === "failed" ? "destructive" : "outline"
                      }
                    >
                      {r.status || "running"}
                    </Badge>
                    <Badge variant="outline" className="font-mono">
                      {r.slot || "general"}
                    </Badge>
                    {r.profile && <Badge variant="outline">profile:{r.profile}</Badge>}
                    {r.origin && <span className="text-muted-foreground">{r.origin}</span>}
                    <span className="font-mono text-muted-foreground">{r.model}</span>
                    <span className="ml-auto font-mono text-muted-foreground">
                      {(r.ms / 1000).toFixed(1)}s · {when(r.started_at)}
                    </span>
                  </div>
                  <div className="flex flex-wrap gap-3 font-mono text-muted-foreground">
                    <span>in: {r.uncached_input.toLocaleString()}</span>
                    <span>cache r: {r.cache_read.toLocaleString()}</span>
                    <span>cache w: {r.cache_write.toLocaleString()}</span>
                    <span>out: {r.output.toLocaleString()}</span>
                  </div>
                  {r.tools && r.tools.length > 0 && (
                    <div className="flex flex-wrap gap-1 pt-0.5">
                      {Array.from(new Set(r.tools)).map((t) => {
                        const count = r.tools.filter((x) => x === t).length
                        return (
                          <Badge key={`${r.run_id}-${t}`} variant="secondary" className="font-mono">
                            {count > 1 ? `${t} ×${count}` : t}
                          </Badge>
                        )
                      })}
                    </div>
                  )}
                  {r.reason && <div className="text-destructive">{r.reason}</div>}
                </div>
              ))}
            </div>
          </TabsContent>

          <TabsContent value="memory" className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto pt-2 text-xs">
            <div className="flex flex-col gap-1 rounded-lg border p-3">
              <div className="flex items-center justify-between">
                <span className="font-medium">Standing Instructions (context/standing.md)</span>
                <span className="font-mono text-muted-foreground">
                  {(standing?.chars ?? 0).toLocaleString()} / 16,000 chars
                </span>
              </div>
              {standing?.path && <code className="font-mono text-muted-foreground">{standing.path}</code>}
              <pre className="mt-1 max-h-40 overflow-auto rounded border bg-muted/40 p-2 font-mono whitespace-pre-wrap">
                {standing?.text || "(no standing instructions file yet)"}
              </pre>
            </div>

            <div className="flex flex-col gap-1.5">
              <span className="font-medium">Slot Notes (saved by context_note)</span>
              {slots.filter((s) => s.note).length === 0 && (
                <p className="text-muted-foreground">No slot notes saved yet.</p>
              )}
              {slots
                .filter((s) => s.note)
                .map((s) => (
                  <div key={s.name} className="flex flex-col gap-1 rounded-md border p-2.5">
                    <div className="flex items-center justify-between">
                      <Badge variant={s.active ? "default" : "outline"}>{s.name}</Badge>
                      <span className="text-muted-foreground">{s.about}</span>
                    </div>
                    <pre className="m-0 font-mono whitespace-pre-wrap text-foreground">{s.note}</pre>
                  </div>
                ))}
            </div>

            <div className="flex flex-col gap-1.5">
              <span className="font-medium">Standing Tool Grants ({grants.length})</span>
              {grants.length === 0 ? (
                <p className="text-muted-foreground">No standing tool approvals granted yet.</p>
              ) : (
                <div className="flex flex-wrap gap-1.5">
                  {grants.map((g) => (
                    <Badge key={`${g.agent}-${g.tool}-${g.created}`} variant="secondary" className="font-mono">
                      {g.tool} ({when(g.created)})
                    </Badge>
                  ))}
                </div>
              )}
            </div>
          </TabsContent>

          <TabsContent value="capabilities" className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto pt-2 text-xs">
            <div className="flex flex-col gap-1.5 rounded-lg border p-3">
              <span className="font-medium">Module Agent Profiles ({profiles?.profiles.length ?? 0})</span>
              {(!profiles || profiles.profiles.length === 0) && (
                <p className="text-muted-foreground">No module profiles registered in this app.</p>
              )}
              {profiles?.profiles.map((p) => (
                <div key={p.id} className="flex flex-col gap-1 border-t pt-2 first:border-t-0 first:pt-0">
                  <div className="flex items-center gap-2">
                    <Badge variant={p.active ? "default" : "outline"}>{p.id}</Badge>
                    <span className="font-medium">{p.title}</span>
                    {p.description && <span className="text-muted-foreground">— {p.description}</span>}
                  </div>
                  <div className="flex flex-wrap gap-1">
                    {p.skills?.map((sk) => (
                      <Badge key={sk} variant="secondary" className="font-mono">
                        skill:{sk}
                      </Badge>
                    ))}
                    {p.variants?.map((v) => (
                      <Badge key={v} variant="outline" className="font-mono">
                        variant:{v}
                      </Badge>
                    ))}
                    {p.tools?.map((t) => (
                      <Badge key={t} variant="outline" className="font-mono">
                        tool:{t}
                      </Badge>
                    ))}
                  </div>
                </div>
              ))}
            </div>

            <div className="flex flex-col gap-1.5 rounded-lg border p-3">
              <span className="font-medium">Reflexes ({reflexes.length})</span>
              {reflexes.length === 0 && <p className="text-muted-foreground">No reflexes configured.</p>}
              {reflexes.map((rx) => (
                <div
                  key={rx.name}
                  className="flex flex-wrap items-center gap-2 border-t pt-1.5 first:border-t-0 first:pt-0"
                >
                  <Badge variant={rx.enabled ? "secondary" : "outline"}>{rx.name}</Badge>
                  <span className="font-mono text-muted-foreground">
                    on {rx.trigger_kind} → {rx.action_kind}
                  </span>
                  {rx.about && <span className="text-muted-foreground">{rx.about}</span>}
                </div>
              ))}
            </div>

            <div className="flex flex-col gap-1.5 rounded-lg border p-3">
              <span className="font-medium">
                Routing Rules ({rules.owner.length} owner, {rules.builtin.length} built-in)
              </span>
              {[...rules.owner, ...rules.builtin].map((rl) => (
                <div
                  key={rl.id}
                  className="flex flex-wrap items-center gap-2 border-t pt-1.5 first:border-t-0 first:pt-0"
                >
                  <Badge variant={rl.disabled ? "outline" : "secondary"} className="font-mono">
                    {rl.id}
                  </Badge>
                  <span className="font-mono text-muted-foreground">→ {rl.target}</span>
                </div>
              ))}
            </div>
          </TabsContent>

          <TabsContent value="trace" className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto pt-2 text-xs">
            {replay && (
              <div className="flex flex-wrap items-center gap-3 rounded-lg border p-2.5">
                <span className="font-medium">Classifier Replay:</span>
                <span>{replay.turns} turns</span>
                {replay.agreed !== undefined && <span>· agreed: {replay.agreed}</span>}
                {replay.corrected !== undefined && (
                  <span>
                    · corrections: {replay.corrected_hits ?? 0}/{replay.corrected}
                  </span>
                )}
              </div>
            )}

            <div className="flex flex-col gap-1.5">
              {trace.turns.length === 0 && <p className="text-muted-foreground">No traced turns yet.</p>}
              {trace.turns.map((t) => (
                <div key={t.id} className="flex flex-col gap-1 rounded-md border p-2.5">
                  <div className="flex items-center gap-2">
                    <Badge variant="outline" className="font-mono">
                      {t.slot || "none"}
                    </Badge>
                    <span className="text-muted-foreground">({t.why || "default"})</span>
                    <span className="font-mono text-muted-foreground">{t.block_chars.toLocaleString()} chars</span>
                    <span className="ml-auto text-muted-foreground">{when(t.at)}</span>
                    <Button
                      variant="ghost"
                      size="xs"
                      title="Send a classifier training/correction card for this turn"
                      onClick={() => askSlotCorrectionCard(t.text, t.slot)}
                    >
                      Feedback card
                    </Button>
                  </div>
                  <p className="truncate text-foreground">{t.text}</p>
                </div>
              ))}
            </div>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
