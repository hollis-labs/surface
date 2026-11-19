/**
 * Design's windows: the list of jobs, and one job's review pane (the preview of its branch in a
 * frame, the base beside it at a switch, the changed files, the notes for the next round).
 */
import {
  addMark,
  Icon,
  type Mark,
  type ModuleWeb,
  resolveMark,
  useEvent,
  useMarks,
  type ViewProps,
} from "@hollis-labs/surface-page/sdk"
import { type ReactElement, type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Item, ItemActions, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item"
import { Markdown } from "@/components/ui/markdown"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Textarea } from "@/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"

type Status = "requested" | "in progress" | "ready for review" | "changes requested" | "approved" | "merged" | "closed"

interface Round {
  n: number
  asked: string
  sent: boolean
  message_id?: string
  created: string
}

interface Reply {
  message_id: string
  from: string
  subject?: string
  body: string
  sent?: string
  received: string
  preview_url?: string
  base_url?: string
  pr_url?: string
  head_sha?: string
  ignored?: string
  note?: string
}

interface Job {
  id: string
  title: string
  brief: string
  refs?: string
  branch: string
  pr_url?: string
  preview_url?: string
  base_url?: string
  status: Status
  task_id?: string
  head_sha?: string
  task_status?: string
  task_comment?: string
  task_checked?: string
  inbox_checked?: string
  approved_head?: string
  approved_pr?: string
  approved_at?: string
  latest_head?: string
  head_differs?: boolean
  update_received?: boolean
  merge_warning?: string
  approval_owed?: boolean
  closed_reason?: string
  rounds: Round[]
  replies: Reply[]
  created: string
  updated: string
}

interface Progress {
  exists: boolean
  head?: string
  head_message?: string
  head_date?: string
  files: string[]
  pr?: { number: number; url: string; state: string; draft?: boolean }
  problem?: string
}

interface JobView extends Job {
  progress: Progress
}

interface Probe {
  url: string
  reachable: boolean
  problem?: string
}

/** The gallery's sections, as the standalone page names them in its hash. */
const SECTIONS = ["draft", "theme", "controls", "display", "overlays", "chat", "cards", "genui", "ade", "windows"]

const STATUS_VARIANT: Record<Status, "default" | "secondary" | "outline" | "destructive"> = {
  requested: "outline",
  "in progress": "secondary",
  "ready for review": "default",
  "changes requested": "destructive",
  approved: "default",
  merged: "secondary",
  closed: "outline",
}

function Tip({ label, children }: { label: ReactNode; children: ReactElement }) {
  return (
    <Tooltip>
      <TooltipTrigger render={children} />
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

function StatusBadge({ status }: { status: Status }) {
  return <Badge variant={STATUS_VARIANT[status] ?? "outline"}>{status}</Badge>
}

const when = (iso?: string) => (iso ? new Date(iso).toLocaleString([], { dateStyle: "short", timeStyle: "short" }) : "")

// ---- The list of jobs -------------------------------------------------------------------------

/** Close a job by hand, after the owner confirms: it leaves the Open list (still under All), and
 * the Design team is told to stop and close the pull request without merging. */
async function closeJob(desk: ViewProps["desk"], api: ViewProps["api"], j: Job): Promise<boolean> {
  const ok = await desk.confirm({
    title: `Close “${j.title}”?`,
    message:
      "It leaves the Open list (it stays under All). The Design team is told to stop and to close the pull request without merging" +
      (j.status === "approved" ? "; the approval no longer stands." : "."),
    confirmLabel: "Close job",
    danger: true,
  })
  if (!ok) return false
  await api.put(`jobs/${j.id}`, { status: "closed" })
  return true
}

const closable = (status: Status) => status !== "merged" && status !== "closed"

function Jobs({ desk, api, win }: ViewProps) {
  const [jobs, setJobs] = useState<Job[]>([])
  const [all, setAll] = useState(false)
  const [error, setError] = useState("")
  const load = useCallback(() => {
    api
      .get<Job[]>(`jobs${all ? "?all=1" : ""}`)
      .then(setJobs)
      .catch((e: Error) => setError(e.message))
  }, [api, all])
  useEffect(load, [load])
  useEvent("design.changed", load)
  useEffect(() => desk.report(win.id, { kind: "data", ref: "design:jobs" }), [desk, win.id])

  const open = (j: Job) => void desk.open("design.job", { key: j.id, title: `Design: ${j.title}`, args: { id: j.id } })

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-2 border-b px-3 py-2">
        <span className="text-sm font-semibold">Design jobs</span>
        <span className="text-xs text-muted-foreground">Ask the agent for a design change to start one.</span>
        <ToggleGroup
          size="sm"
          spacing={0}
          className="ml-auto"
          value={[all ? "all" : "open"]}
          onValueChange={(v: string[]) => v[0] && setAll(v[0] === "all")}
        >
          <ToggleGroupItem value="open">Open</ToggleGroupItem>
          <ToggleGroupItem value="all">All</ToggleGroupItem>
        </ToggleGroup>
      </div>
      {error && <p className="px-3 py-2 text-sm text-destructive">{error}</p>}
      <div className="min-h-0 flex-1 overflow-auto p-2">
        {jobs.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>No design jobs</EmptyTitle>
              <EmptyDescription>
                Tell the agent what should change in the interface; it hands the work to the Design team and the job
                shows here.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className="flex flex-col gap-1">
            {jobs.map((j) => (
              <Item
                key={j.id}
                variant="outline"
                size="sm"
                render={<a href={`#design-${j.id}`} />}
                onClick={(e) => {
                  e.preventDefault()
                  open(j)
                }}
              >
                <ItemContent>
                  <ItemTitle>{j.title}</ItemTitle>
                  <ItemDescription className="font-mono text-xs">
                    {j.id} · {j.branch}
                  </ItemDescription>
                </ItemContent>
                <ItemActions>
                  <span className="text-xs text-muted-foreground">{when(j.updated)}</span>
                  <StatusBadge status={j.status} />
                  {closable(j.status) && (
                    <Tip label="Close this job">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        aria-label={`Close ${j.title}`}
                        onClick={(e) => {
                          e.preventDefault()
                          e.stopPropagation()
                          closeJob(desk, api, j)
                            .then((done) => done && load())
                            .catch((err: Error) => setError(err.message))
                        }}
                      >
                        <Icon name="x" />
                      </Button>
                    </Tip>
                  )}
                </ItemActions>
              </Item>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

// ---- One job: the review pane -----------------------------------------------------------------

/** The URL the frame loads: the preview page with the theme, the job (whose draft the Draft tab
 * shows) and the section. */
function frameURL(base: string, theme: string, section: string, job: string) {
  try {
    const u = new URL(base)
    u.searchParams.set("theme", theme)
    u.searchParams.set("job", job)
    u.hash = section
    return u.toString()
  } catch {
    return ""
  }
}

function JobPane({ win, desk, api }: ViewProps) {
  const id = String(win.args.id ?? "")
  const [job, setJob] = useState<JobView | null>(null)
  const [error, setError] = useState("")
  const [showing, setShowing] = useState<"branch" | "base">("branch")
  const [theme, setTheme] = useState<"dark" | "light">(
    document.documentElement.classList.contains("dark") ? "dark" : "light",
  )
  const [section, setSection] = useState(typeof win.args.section === "string" ? win.args.section : "draft")
  const [probe, setProbe] = useState<Probe | null>(null)
  const [reloads, setReloads] = useState(0)
  const [busy, setBusy] = useState("")
  const [outcome, setOutcome] = useState("")
  const [details, setDetails] = useState(false)
  const [note, setNote] = useState<NoteDraft>({ body: "", component: "" })
  const frame = useRef<HTMLIFrameElement>(null)
  const marks = useMarks(id ? `design:${id}` : undefined)

  const load = useCallback(
    (fresh = false) => {
      if (!id) return
      api
        .get<JobView>(`jobs/${id}${fresh ? "?fresh=1" : ""}`)
        .then((j) => {
          setJob(j)
          setError("")
        })
        .catch((e: Error) => setError(e.message))
    },
    [api, id],
  )
  useEffect(() => load(true), [load])
  // The branch moves on GitHub: read it while the window is open (answers are cached briefly).
  useEffect(() => {
    const t = setInterval(() => load(), 30_000)
    return () => clearInterval(t)
  }, [load])
  useEvent(
    "design.changed",
    useCallback((ev: { data: unknown }) => (ev.data as { id?: string })?.id === id && load(), [id, load]),
  )

  const target = showing === "base" ? job?.base_url : job?.preview_url

  // Whether the preview answers. A framed page from another origin cannot tell us, so the
  // server asks. When it comes back, the frame is loaded again.
  useEffect(() => {
    if (!job) return
    let live = true
    let was: boolean | null = null
    const check = () =>
      api
        .get<Probe>(`jobs/${id}/probe${showing === "base" ? "?which=base" : ""}`)
        .then((p) => {
          if (!live) return
          if (was === false && p.reachable) setReloads((n) => n + 1)
          was = p.reachable
          setProbe(p)
        })
        .catch(() => undefined)
    void check()
    const t = setInterval(check, 10_000)
    return () => {
      live = false
      clearInterval(t)
    }
  }, [api, id, job?.preview_url, job?.base_url, showing, job])

  // The gallery in the frame says which section it shows.
  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      if (e.source !== frame.current?.contentWindow) return
      const d = e.data as { source?: string; section?: string }
      if (d?.source === "surface-gallery" && d.section && SECTIONS.includes(d.section)) setSection(d.section)
    }
    window.addEventListener("message", onMessage)
    return () => window.removeEventListener("message", onMessage)
  }, [])

  // What the window shows, for the agent and the context slot.
  useEffect(() => {
    desk.report(win.id, { showing, section, theme })
  }, [desk, win.id, showing, section, theme])

  const src = useMemo(() => (target ? frameURL(target, theme, section, id) : ""), [target, theme, section, id])

  const run = (label: string, p: Promise<unknown>, done: (r: unknown) => string) => {
    setBusy(label)
    setOutcome("")
    p.then((r) => setOutcome(done(r)))
      .catch((e: Error) => setOutcome(e.message))
      .finally(() => setBusy(""))
  }

  if (!id) return <p className="p-4 text-sm text-muted-foreground">No job given.</p>
  if (!job) return <p className="p-4 text-sm text-muted-foreground">{error || "Reading the job…"}</p>

  const open = marks.filter((m) => m.status === "open")
  const settledJob = ["approved", "merged", "closed"].includes(job.status)
  const canSend = busy === "" && open.length > 0 && !settledJob
  const canApprove = busy === "" && !!job.head_sha && !!job.pr_url && !settledJob
  const sendNotes = () =>
    run("feedback", api.post(`jobs/${job.id}/feedback`, {}), (r) => {
      const o = (r as { outbox: { sent: boolean; problem?: string } }).outbox
      return o.sent ? "Sent as the next round." : `Not sent: ${o.problem}. Ask the agent for the text.`
    })
  const approve = () => run("approve", api.post(`jobs/${job.id}/approve`), () => "Approve it on the card.")
  const approveHint =
    (job.approved_head
      ? `Approved: ${job.approved_pr?.replace(/^https:\/\/github\.com\//, "") ?? ""} at ${job.approved_head.slice(0, 7)}${job.head_differs ? `; a new commit ${job.latest_head?.slice(0, 7)} came after the approval` : ""}`
      : job.head_sha
        ? `Approving covers commit ${job.head_sha.slice(0, 7)}${job.pr_url ? ` (${job.pr_url.replace(/^https:\/\/github\.com\//, "")})` : ""} only`
        : "Nothing to approve until the Designer names a head and a pull request") +
    (job.update_received
      ? job.preview_url
        ? " · new work came back on the branch"
        : " · an update came back without a preview"
      : "") +
    (job.approval_owed ? " · waiting for GitHub to show this commit; you are asked to approve on your own" : "")

  return (
    <div className="@container flex h-full min-h-0 flex-col">
      <header className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
        <span className="min-w-0 truncate text-sm font-semibold">{job.title}</span>
        <StatusBadge status={job.status} />
        <span className="font-mono text-xs text-muted-foreground @max-lg:hidden">
          {job.branch}
          {job.progress.head ? ` @ ${job.progress.head.slice(0, 7)}` : ""}
        </span>
        {/* Wraps in a narrow window, so Send notes, Approve and Details are always in reach. */}
        <div className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-2">
          <ToggleGroup
            size="sm"
            spacing={0}
            value={[showing]}
            onValueChange={(v: string[]) => v[0] && setShowing(v[0] as "branch" | "base")}
          >
            <ToggleGroupItem value="branch">Branch</ToggleGroupItem>
            <ToggleGroupItem value="base" disabled={!job.base_url}>
              Base
            </ToggleGroupItem>
          </ToggleGroup>
          <ToggleGroup
            size="sm"
            spacing={0}
            value={[theme]}
            onValueChange={(v: string[]) => v[0] && setTheme(v[0] as "dark" | "light")}
          >
            <ToggleGroupItem value="dark" aria-label="Dark">
              <Icon name="moon" />
            </ToggleGroupItem>
            <ToggleGroupItem value="light" aria-label="Light">
              <Icon name="sun" />
            </ToggleGroupItem>
          </ToggleGroup>
          <Select
            value={section}
            onValueChange={(v) => v && setSection(String(v))}
            items={SECTIONS.map((s) => ({ value: s, label: s }))}
          >
            <SelectTrigger size="sm" aria-label="Section">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {SECTIONS.map((s) => (
                <SelectItem key={s} value={s}>
                  {s}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Tip label="Read the branch and the pull request again">
            <Button variant="ghost" size="icon-sm" aria-label="Refresh" onClick={() => load(true)}>
              <Icon name="refresh-cw" />
            </Button>
          </Tip>
          <Separator orientation="vertical" className="h-5" />
          <Tip label={open.length ? `Send ${open.length} open note(s) as the next round` : "No open notes to send"}>
            {/* The span takes the pointer when the button is disabled, so the tooltip still says why. */}
            <span className="inline-flex">
              <Button size="sm" variant="outline" disabled={!canSend} onClick={sendNotes}>
                Send notes{open.length ? ` (${open.length})` : ""}
              </Button>
            </span>
          </Tip>
          <Tip label={approveHint}>
            <span className="inline-flex">
              <Button size="sm" disabled={!canApprove} onClick={approve}>
                Approve…
              </Button>
            </span>
          </Tip>
          <Button size="sm" variant="ghost" onClick={() => setDetails(true)}>
            <Icon name="panel-right" />
            Details{note.body.trim() ? " (note unsent)" : ""}
          </Button>
        </div>
      </header>
      {(outcome || job.merge_warning || job.approval_owed || job.update_received) && (
        <div className="flex flex-col gap-0.5 border-b px-3 py-1 text-xs text-muted-foreground">
          {job.merge_warning && <p className="font-medium text-destructive">{job.merge_warning}</p>}
          {job.approval_owed && <p>Waiting for GitHub to show this commit; you are asked to approve on your own.</p>}
          {job.update_received && (
            <p>{job.preview_url ? "New work came back on the branch." : "An update came back without a preview."}</p>
          )}
          {outcome && <p>{outcome}</p>}
        </div>
      )}

      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1 bg-muted/30">
          {src && probe?.reachable !== false ? (
            <iframe
              key={`${showing}-${reloads}-${theme}`}
              ref={frame}
              src={src}
              title={`${job.title}: ${showing}`}
              className="size-full border-0"
            />
          ) : (
            <Empty className="h-full">
              <EmptyHeader>
                <EmptyTitle>{target ? "The preview does not answer" : "No preview yet"}</EmptyTitle>
                <EmptyDescription>
                  {target
                    ? `${probe?.problem ?? "It cannot be reached"}: ${target}. It is tried again every few seconds.`
                    : showing === "base"
                      ? "No base preview is set for this job."
                      : "The Design team serves the gallery from their branch with make design-preview and gives its URL; set it here or ask the agent to."}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
        </div>
      </div>

      <Dialog open={details} onOpenChange={setDetails}>
        <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{job.title}</DialogTitle>
            <DialogDescription>
              {job.branch}
              {job.progress.head ? ` @ ${job.progress.head.slice(0, 7)}` : ""}
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-1 text-xs text-muted-foreground">
            <p>{approveHint}</p>
            {job.status === "closed" && job.closed_reason && <p>Closed: {job.closed_reason}</p>}
            {job.merge_warning && <p className="font-medium text-destructive">{job.merge_warning}</p>}
          </div>
          <Separator />
          <Notes
            jobId={job.id}
            section={section}
            marks={open}
            resolved={marks.length - open.length}
            draft={note}
            onDraft={setNote}
          />
          <Separator />
          <Changes job={job} />
          <Separator />
          <Rounds job={job} />
          <Separator />
          <Replies job={job} />
          <Separator />
          <Previews job={job} api={api} onSaved={() => load()} />
          {closable(job.status) && (
            <>
              <Separator />
              <div className="flex items-center justify-between gap-2">
                <span className="text-xs text-muted-foreground">Done with it, or no longer wanted?</span>
                <Button
                  size="sm"
                  variant="destructive"
                  onClick={() =>
                    closeJob(desk, api, job)
                      .then((done) => {
                        if (!done) return
                        setDetails(false)
                        load()
                      })
                      .catch((err: Error) => setOutcome(err.message))
                  }
                >
                  Close job
                </Button>
              </div>
            </>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}

function Section({ title, children, extra }: { title: string; children: ReactNode; extra?: ReactNode }) {
  return (
    <section className="flex flex-col gap-2 p-3">
      <div className="flex items-center gap-2">
        <h3 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{title}</h3>
        <div className="ml-auto">{extra}</div>
      </div>
      {children}
    </section>
  )
}

/** The preview URLs the Design team gave, editable here. */
function Previews({ job, api, onSaved }: { job: Job; api: ViewProps["api"]; onSaved: () => void }) {
  const [preview, setPreview] = useState(job.preview_url ?? "")
  const [base, setBase] = useState(job.base_url ?? "")
  useEffect(() => {
    setPreview(job.preview_url ?? "")
    setBase(job.base_url ?? "")
  }, [job.preview_url, job.base_url])
  const dirty = preview !== (job.preview_url ?? "") || base !== (job.base_url ?? "")
  const [error, setError] = useState("")
  return (
    <Section title="Preview">
      <Input
        aria-label="Preview of the branch"
        placeholder="Branch preview URL"
        value={preview}
        onChange={(e) => setPreview(e.target.value)}
        className="h-7 font-mono text-xs"
      />
      <Input
        aria-label="Preview of the base"
        placeholder="Base preview URL (optional)"
        value={base}
        onChange={(e) => setBase(e.target.value)}
        className="h-7 font-mono text-xs"
      />
      {dirty && (
        <Button
          size="xs"
          onClick={() =>
            api
              .put(`jobs/${job.id}`, { preview_url: preview, base_url: base })
              .then(() => {
                setError("")
                onSaved()
              })
              .catch((e: Error) => setError(e.message))
          }
        >
          Save
        </Button>
      )}
      {error && <p className="text-xs text-destructive">{error}</p>}
    </Section>
  )
}

/** Notes for the next round: marks on the job, pointing at a section (and a component). */
/** A note being written: kept by the window, so closing Details does not lose it. */
interface NoteDraft {
  body: string
  component: string
}

function Notes({
  jobId,
  section,
  marks,
  resolved,
  draft,
  onDraft,
}: {
  jobId: string
  section: string
  marks: Mark[]
  resolved: number
  draft: NoteDraft
  onDraft: (d: NoteDraft) => void
}) {
  const { body, component } = draft
  const setBody = (b: string) => onDraft({ body: b, component })
  const setComponent = (c: string) => onDraft({ body, component: c })
  const add = () => {
    const b = body.trim()
    if (!b) return
    const anchor: { section: string; component?: string } = { section }
    if (component.trim()) anchor.component = component.trim()
    void addMark({ ref: `design:${jobId}`, body: b, anchor }).then(() => onDraft({ body: "", component: "" }))
  }
  return (
    <Section
      title={`Notes (${marks.length})`}
      extra={resolved > 0 ? <span className="text-xs text-muted-foreground">{resolved} sent</span> : undefined}
    >
      {marks.map((m) => {
        const a = (m.anchor ?? {}) as { section?: string; component?: string }
        return (
          <Item key={m.id} variant="muted" size="xs">
            <ItemContent>
              <ItemDescription className="text-xs">
                {a.section ?? "general"}
                {a.component ? ` / ${a.component}` : ""}
              </ItemDescription>
              <Markdown className="w-full">{m.body}</Markdown>
            </ItemContent>
            <ItemActions>
              <Tip label="Drop this note">
                <Button variant="ghost" size="icon-xs" aria-label="Drop" onClick={() => void resolveMark(m.id)}>
                  <Icon name="x" />
                </Button>
              </Tip>
            </ItemActions>
          </Item>
        )
      })}
      <Textarea
        aria-label="A note for the next round"
        placeholder={`A note on ${section}…`}
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) add()
        }}
        className="min-h-16 text-sm"
      />
      <div className="flex gap-2">
        <Input
          aria-label="Component (optional)"
          placeholder="component (optional)"
          value={component}
          onChange={(e) => setComponent(e.target.value)}
          className="h-7 flex-1 text-xs"
        />
        <Button size="sm" variant="secondary" disabled={!body.trim()} onClick={add}>
          Pin note
        </Button>
      </div>
    </Section>
  )
}

function Changes({ job }: { job: JobView }) {
  const p = job.progress
  return (
    <Section
      title={`Changes (${p.files.length})`}
      extra={
        p.pr ? (
          <a
            href={p.pr.url}
            target="_blank"
            rel="noreferrer"
            className="text-xs text-primary underline-offset-2 hover:underline"
          >
            #{p.pr.number} {p.pr.state}
            {p.pr.draft ? " (draft)" : ""}
          </a>
        ) : undefined
      }
    >
      {p.problem && <p className="text-xs text-muted-foreground">{p.problem}</p>}
      {p.head_message && (
        <p className="text-xs">
          <span className="font-mono text-muted-foreground">{p.head?.slice(0, 7)}</span> {p.head_message}
          <span className="text-muted-foreground"> · {when(p.head_date)}</span>
        </p>
      )}
      <ul className="flex flex-col gap-0.5 font-mono text-xs">
        {p.files.map((f) => (
          <li key={f} className="truncate" title={f}>
            {f}
          </li>
        ))}
      </ul>
    </Section>
  )
}

function Rounds({ job }: { job: Job }) {
  return (
    <Section title={`Rounds (${job.rounds.length})`}>
      {job.rounds.map((r) => (
        <div key={r.n} className="flex flex-col gap-0.5 text-xs">
          <span className="text-muted-foreground">
            Round {r.n} · {when(r.created)}
            {r.sent ? "" : " · not sent"}
          </span>
          <p className="line-clamp-4 whitespace-pre-wrap">{r.asked}</p>
        </div>
      ))}
    </Section>
  )
}

/** What came back: the task's state and the Designer's replies, shown as text. */
function Replies({ job }: { job: Job }) {
  const set = (r: Reply) =>
    [
      r.preview_url && "preview",
      r.base_url && "base",
      r.pr_url && "pull request",
      r.head_sha && `head ${r.head_sha.slice(0, 7)}`,
    ].filter(Boolean)
  return (
    <Section title={`From the Designer (${job.replies?.length ?? 0})`}>
      <p className="text-xs text-muted-foreground">
        {job.task_id ? `Task ${job.task_id}: ${job.task_status || "not read yet"}` : "No task"}
        {job.task_checked ? ` · checked ${when(job.task_checked)}` : ""}
        {job.inbox_checked ? ` · mail checked ${when(job.inbox_checked)}` : ""}
      </p>
      {job.task_comment && <p className="line-clamp-4 whitespace-pre-wrap text-xs">{job.task_comment}</p>}
      {(job.replies ?? []).map((r) => (
        <div key={r.message_id} className="flex flex-col gap-0.5 text-xs">
          <span className="text-muted-foreground">
            {when(r.sent || r.received)}
            {set(r).length > 0 ? ` · set ${set(r).join(", ")}` : ""}
          </span>
          <Markdown className="line-clamp-6 text-xs">{r.body}</Markdown>
          {r.note && <p className="whitespace-pre-wrap text-muted-foreground">{r.note}</p>}
          {r.ignored && <p className="whitespace-pre-wrap text-muted-foreground">Not taken: {r.ignored}</p>}
        </div>
      ))}
    </Section>
  )
}

export default { views: { "design/jobs": Jobs, "design/job": JobPane } } satisfies ModuleWeb
