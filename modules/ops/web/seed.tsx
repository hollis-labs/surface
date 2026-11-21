/**
 * Seeding Operations from the owner's workstream tracker. Always a preview first: the tracker
 * (uploaded, pasted, or a path on this machine) is mapped into workstreams, proposed concept
 * groupings and placed follow-ups, and nothing is written. The owner edits the mapping here
 * (which threads, their ids, stages, owners and parents; which concepts to make; where each
 * follow-up goes) and applies it. Concepts are off until ticked.
 */
import { CiteText, Icon, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { message, opsClient, type SeedPlan, type SeedResult, type Stage, stageLabel } from "./api"

const NONE = "\u0000none"
const SEED_STAGES: Stage[] = ["planned", "parked", "queued", "in_flight", "landing", "complete"]

export function SeedView({ win, desk, api }: ViewProps) {
  const ops = useMemo(() => opsClient(api), [api])
  const planID = typeof win.args.plan === "string" ? win.args.plan : ""
  const [plan, setPlan] = useState<SeedPlan | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<SeedResult | null>(null)

  const load = useCallback(async () => {
    if (!planID) return
    try {
      setPlan(await ops.seedGet(planID))
      setError("")
    } catch (e) {
      setError(message(e))
    }
  }, [ops, planID])
  useEffect(() => {
    void load()
  }, [load])

  if (!planID) {
    return (
      <Source
        busy={busy}
        error={error}
        preview={async (src) => {
          setBusy(true)
          try {
            const p = await ops.seedPreview(src)
            await desk.setArgs(win.id, { plan: p.id })
          } catch (e) {
            setError(message(e))
          } finally {
            setBusy(false)
          }
        }}
      />
    )
  }
  if (!plan) {
    return (
      <div className="flex h-full items-center justify-center text-sm">
        {error ? <span className="text-destructive">{error}</span> : <Spinner />}
      </div>
    )
  }

  const edit = (fn: (p: SeedPlan) => void) => {
    const next = structuredClone(plan)
    fn(next)
    setPlan(next)
  }
  const accepted = new Set(plan.concepts.filter((c) => c.accept).map((c) => c.id))
  const included = plan.workstreams.filter((w) => w.include)
  const parents = [
    ...plan.concepts.map((c) => ({ id: c.id, label: `${c.title} (concept${c.accept ? "" : ", not ticked"})` })),
    ...included.map((w) => ({ id: w.id, label: w.title })),
  ]
  const forOwner = plan.followups.filter((f) => !f.workstream || !included.some((w) => w.id === f.workstream)).length
  const applied = !!plan.applied_at

  const save = async () => {
    try {
      setPlan(await ops.seedSave(plan))
      setError("")
    } catch (e) {
      setError(message(e))
    }
  }
  const apply = async () => {
    const ok = await desk.confirm({
      title: "Apply this seed?",
      message: `Writes ${included.length + accepted.size} workstreams to Caster (namespace ops)${accepted.size ? `, ${accepted.size} of them concepts` : ""}, and ${forOwner} follow-ups to your list. Ids Ops has already are skipped, never overwritten.`,
      confirmLabel: "Apply",
    })
    if (!ok) return
    setBusy(true)
    try {
      await ops.seedSave(plan)
      setResult(await ops.seedApply(plan.id))
      setError("")
      await load()
    } catch (e) {
      setError(message(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-2">
        <Icon name="sprout" className="size-4" />
        <span className="font-semibold text-sm">Seed preview</span>
        <span className="text-muted-foreground text-xs">
          {plan.source} · tracker as of {plan.tracker_updated || "?"} · {included.length} of {plan.workstreams.length}{" "}
          threads · {accepted.size} concepts ticked · {forOwner} follow-ups for you
        </span>
        <span className="ml-auto" />
        {applied ? (
          <Badge variant="secondary">applied {new Date(plan.applied_at ?? "").toLocaleString()}</Badge>
        ) : (
          <>
            <Button size="sm" variant="ghost" onClick={() => void save()} disabled={busy}>
              Save edits
            </Button>
            <Button size="sm" onClick={() => void apply()} disabled={busy || included.length === 0}>
              {busy ? <Spinner /> : <Icon name="check" />}
              Apply
            </Button>
          </>
        )}
        <Button size="sm" variant="ghost" onClick={() => void desk.run("ops.open", {})}>
          Board
        </Button>
      </div>
      {error && <p className="shrink-0 border-b px-3 py-1.5 text-destructive text-xs">{error}</p>}
      {result && (
        <p className="shrink-0 border-b px-3 py-1.5 text-xs" data-seed-result>
          Created {result.created.length}, skipped {result.skipped.length} (already in Ops), {result.failed.length}{" "}
          failed, {result.followups_for_owner} follow-ups on your list
          {result.noted.length > 0 ? `, follow-ups added to ${result.noted.length} workstreams already in Ops` : ""}.
          {result.failed.map((f) => (
            <span key={f} className="block text-destructive">
              {f}
            </span>
          ))}
        </p>
      )}
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-3 text-sm">
        <section aria-label="Mapping">
          <h3 className="mb-1 font-medium">How the tracker maps</h3>
          <ul className="flex list-disc flex-col gap-0.5 pl-4 text-muted-foreground text-xs">
            {plan.mapping.map((m) => (
              <li key={m}>{m}</li>
            ))}
          </ul>
        </section>
        <section aria-label="Proposed concepts">
          <h3 className="mb-1 font-medium">Proposed concepts (tick to make them, and nest their threads under them)</h3>
          <div className="flex flex-col gap-2">
            {plan.concepts.map((c, i) => (
              <div key={c.id} className="flex items-start gap-2 rounded-md border p-2" data-concept={c.id}>
                <Checkbox
                  checked={c.accept}
                  disabled={applied}
                  aria-label={`Make ${c.title}`}
                  onCheckedChange={(on) =>
                    edit((p) => {
                      p.concepts[i].accept = on === true
                    })
                  }
                />
                <div className="flex min-w-0 flex-col gap-1">
                  <span className="font-medium">
                    {c.title} <span className="font-mono text-muted-foreground text-xs">ops:{c.id}</span>{" "}
                    <Badge variant="outline">{stageLabel(c.stage)}</Badge>
                    {c.exists && <Badge variant="secondary">already in Ops</Badge>}
                  </span>
                  <span className="text-muted-foreground text-xs">{c.why}</span>
                  <span className="flex flex-wrap gap-1">
                    {plan.workstreams
                      .filter((w) => w.parent === c.id)
                      .map((w) => (
                        <Badge key={w.id} variant="secondary">
                          {w.title}
                        </Badge>
                      ))}
                  </span>
                </div>
              </div>
            ))}
            {plan.concepts.length === 0 && <p className="text-muted-foreground text-xs">No groupings proposed.</p>}
          </div>
        </section>
        <section aria-label="Workstreams">
          <h3 className="mb-1 font-medium">Workstreams</h3>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-8" />
                <TableHead>Thread → id</TableHead>
                <TableHead>Stage</TableHead>
                <TableHead>Owner</TableHead>
                <TableHead>Inside</TableHead>
                <TableHead>Tasks</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {plan.workstreams.map((w, i) => (
                <TableRow key={w.title} data-seed-row={w.id}>
                  <TableCell className="align-top">
                    <Checkbox
                      checked={w.include}
                      disabled={applied || w.exists}
                      aria-label={`Include ${w.title}`}
                      onCheckedChange={(on) =>
                        edit((p) => {
                          p.workstreams[i].include = on === true
                        })
                      }
                    />
                  </TableCell>
                  <TableCell className="max-w-96 whitespace-normal align-top">
                    <div className="font-medium">{w.title}</div>
                    <Input
                      value={w.id}
                      disabled={applied}
                      aria-label={`Id of ${w.title}`}
                      className="mt-1 h-7 font-mono text-xs"
                      onChange={(e) =>
                        edit((p) => {
                          const old = p.workstreams[i].id
                          const next = e.target.value
                          p.workstreams[i].id = next
                          for (const x of p.workstreams) if (x.parent === old) x.parent = next
                          for (const f of p.followups) if (f.workstream === old) f.workstream = next
                        })
                      }
                    />
                    {w.problems?.map((pr) => (
                      <div key={pr} className="text-warning text-xs">
                        {pr}
                      </div>
                    ))}
                    {w.notes && (
                      <div className="mt-1 line-clamp-3 text-muted-foreground text-xs" title={w.notes}>
                        {w.notes}
                      </div>
                    )}
                  </TableCell>
                  <TableCell className="align-top">
                    <span className="mb-1 block text-muted-foreground text-xs">
                      tracker: {w.from_stage}
                      {w.phase ? ` (${w.phase})` : ""}
                    </span>
                    <Select
                      value={w.stage}
                      disabled={applied}
                      onValueChange={(v) =>
                        edit((p) => {
                          p.workstreams[i].stage = String(v) as Stage
                        })
                      }
                    >
                      <SelectTrigger size="sm" aria-label={`Stage of ${w.title}`}>
                        <SelectValue>{(v: string) => stageLabel(v)}</SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        {SEED_STAGES.map((s) => (
                          <SelectItem key={s} value={s}>
                            {stageLabel(s)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </TableCell>
                  <TableCell className="align-top">
                    <Input
                      value={w.owner ?? ""}
                      disabled={applied}
                      aria-label={`Owner of ${w.title}`}
                      className="h-7 w-44 text-xs"
                      onChange={(e) =>
                        edit((p) => {
                          p.workstreams[i].owner = e.target.value
                        })
                      }
                    />
                  </TableCell>
                  <TableCell className="align-top">
                    <Select
                      value={w.parent || NONE}
                      disabled={applied}
                      onValueChange={(v) =>
                        edit((p) => {
                          p.workstreams[i].parent = String(v) === NONE ? "" : String(v)
                        })
                      }
                    >
                      <SelectTrigger size="sm" aria-label={`Parent of ${w.title}`} className="w-52">
                        <SelectValue>
                          {(v: string) => (v === NONE ? "Top level" : (parents.find((x) => x.id === v)?.label ?? v))}
                        </SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value={NONE}>Top level</SelectItem>
                        {parents
                          .filter((x) => x.id !== w.id)
                          .map((x) => (
                            <SelectItem key={x.id} value={x.id}>
                              {x.label}
                            </SelectItem>
                          ))}
                      </SelectContent>
                    </Select>
                    {w.parent && !accepted.has(w.parent) && plan.concepts.some((c) => c.id === w.parent) && (
                      <span className="block text-muted-foreground text-xs">
                        stays top level unless the concept is ticked
                      </span>
                    )}
                  </TableCell>
                  <TableCell className="max-w-72 whitespace-normal align-top text-xs">
                    {w.links.length > 0 ? (
                      <CiteText text={w.links.map((l) => l.id).join(" ")} />
                    ) : (
                      <span className="text-muted-foreground">none</span>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </section>
        <section aria-label="Follow-ups">
          <h3 className="mb-1 font-medium">Follow-ups</h3>
          <div className="flex flex-col gap-1.5">
            {plan.followups.map((f, i) => (
              <div key={f.index} className="flex items-start gap-2 border-b pb-1.5 text-xs">
                <Select
                  value={f.workstream || NONE}
                  disabled={applied}
                  onValueChange={(v) =>
                    edit((p) => {
                      p.followups[i].workstream = String(v) === NONE ? "" : String(v)
                      p.followups[i].why = "set by you"
                    })
                  }
                >
                  <SelectTrigger size="sm" aria-label={`Where follow-up ${f.index + 1} goes`} className="w-64 shrink-0">
                    <SelectValue>
                      {(v: string) =>
                        v === NONE ? "Your list" : (plan.workstreams.find((w) => w.id === v)?.title ?? v)
                      }
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={NONE}>Your list</SelectItem>
                    {included.map((w) => (
                      <SelectItem key={w.id} value={w.id}>
                        {w.title}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <span className="min-w-0 flex-1">
                  <CiteText text={f.text} />
                  {f.why && <span className="ml-1 text-muted-foreground">({f.why})</span>}
                </span>
              </div>
            ))}
          </div>
        </section>
      </div>
    </div>
  )
}

function Source({
  busy,
  error,
  preview,
}: {
  busy: boolean
  error: string
  preview: (src: { path?: string; json?: string }) => Promise<void>
}) {
  const [text, setText] = useState("")
  const [file, setFile] = useState("")
  return (
    <div className="flex h-full min-h-0 flex-col gap-3 overflow-y-auto bg-background p-4 text-sm">
      <h2 className="flex items-center gap-2 font-semibold text-base">
        <Icon name="sprout" className="size-4" />
        Seed Operations from the tracker
      </h2>
      <p className="text-muted-foreground text-xs">
        The tracker is read as data and mapped into workstreams. You get a preview to edit; nothing is written to Caster
        until you apply it.
      </p>
      <Label className="flex flex-col items-start gap-1 text-xs">
        Upload the tracker's JSON
        <Input
          type="file"
          accept=".json,application/json"
          className="max-w-md"
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (!f) return
            setFile(f.name)
            void f.text().then(setText)
          }}
        />
      </Label>
      <Label className="flex flex-col items-start gap-1 text-xs">
        Or paste its JSON, or an absolute path to it on this machine
        <Textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          rows={10}
          className="font-mono text-xs"
          placeholder='{"threads": [...], "followups": [...]}'
        />
      </Label>
      {file && <span className="text-muted-foreground text-xs">Read {file}</span>}
      {error && <p className="text-destructive text-xs">{error}</p>}
      <span>
        <Button
          disabled={busy || !text.trim()}
          onClick={() => {
            const t = text.trim()
            void preview(t.startsWith("{") ? { json: t } : { path: t })
          }}
        >
          {busy ? <Spinner /> : <Icon name="eye" />}
          Preview
        </Button>
      </span>
    </div>
  )
}
