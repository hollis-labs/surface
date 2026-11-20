/**
 * The Agent Workbench: the agent library on the left; on the right, the agent's parts as
 * tabs (prompt, skills, tools, variants, providers, reflexes and triggers, context), its
 * history, its evals (suites, runs, comparison, improvement proposals) and the assembled
 * prompt. Edits are kept in the page until Save, which validates on the server and keeps a
 * version; nothing is saved that does not validate.
 */
import { ApiError, type ModuleAPI, type ModuleWeb, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import {
  Copy,
  Download,
  FlaskConical,
  FolderInput,
  Play,
  Plus,
  RefreshCw,
  RotateCcw,
  Save,
  Sparkles,
  Trash2,
  Upload,
} from "lucide-react"
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Item, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item"
import { Markdown } from "@/components/ui/markdown"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { TextEditor } from "@/components/ui/text-editor"
import { Textarea } from "@/components/ui/textarea"

// ---- Types (the module's JSON) ----

interface Agent {
  id: string
  name: string
  description?: string
  digest: string
  version: number
  updated: string
  ok: boolean
  source: string
}
interface Problem {
  file?: string
  message: string
  severity: "error" | "warning"
}
interface Report {
  ok: boolean
  digest: string
  agentdir_digest?: string
  problems: Problem[]
  summary?: { name: string; kind: string; model?: string; skills: string[]; variants: string[]; context: string[] }
}
interface Version {
  n: number
  digest: string
  note: string
  author: string
  created: string
  files: number
}
interface AgentView {
  agent: Agent
  files: Record<string, string>
  report: Report
  versions: Version[]
  dir: string
}
interface DiffLine {
  op: " " | "+" | "-"
  text: string
}
interface FileDiff {
  file: string
  change: string
  lines: DiffLine[]
}
interface Template {
  name: string
  about: string
}
interface SuiteInfo {
  name: string
  description?: string
  cases: number
  error?: string
  source: string
}
interface Caps {
  max_usd: number
  turn_tokens: number
  run_tokens: number
}
interface Estimate {
  cases: number
  model: string
  prompt_tokens: number
  tokens: number
  caps: Caps
  worst_usd: number
  gateway: string
  ready: boolean
  not_ready?: string
}
interface GraderResult {
  kind: string
  pass: boolean
  skipped?: boolean
  detail?: string
}
interface ToolUse {
  name: string
  input?: Record<string, unknown>
}
interface Exchange {
  sent: string
  reply?: string
  calls?: ToolUse[]
  subtask?: boolean
  tokens: number
}
interface CaseResult {
  stub_calls?: Record<string, number>
  id: string
  pass: boolean
  why?: string
  skipped?: string
  graders: GraderResult[]
  answer: string
  transcript: Exchange[]
  steps: number
  tokens: number
  wall_ms: number
}
interface RunInfo {
  id: string
  mode?: string
  suite: string
  digest: string
  version: number
  status: string
  started: string
  passed: number
  failed: number
  tokens: number
  spent_usd: number
  stopped?: string
  previous?: string
}
interface Run extends RunInfo {
  mode: string
  model: string
  caps: Caps
  cases: CaseResult[]
}
interface Comparison {
  a: RunInfo
  b: RunInfo
  fixed: number
  broken: number
  cases: {
    id: string
    a?: boolean
    b?: boolean
    a_tokens: number
    b_tokens: number
    a_steps: number
    b_steps: number
    change: string
  }[]
}
interface Edit {
  n: number
  file: string
  diff: string
  rationale: string
  status: string
  problem?: string
  version?: number
}
interface Proposal {
  id: string
  run: string
  created: string
  model: string
  tokens: number
  error?: string
  items: Edit[]
}
interface PreviewSlot {
  name: string
  content: string
  tokens: number
  read_only: boolean
}
interface Preview {
  slots: PreviewSlot[]
  tools: string[]
  tokens: number
}

type Files = Record<string, string>
type Tab =
  | "prompt"
  | "skills"
  | "tools"
  | "variants"
  | "providers"
  | "steering"
  | "context"
  | "history"
  | "evals"
  | "preview"
const TABS: { tab: Tab; label: string }[] = [
  { tab: "prompt", label: "Prompt" },
  { tab: "skills", label: "Skills" },
  { tab: "tools", label: "Tools" },
  { tab: "variants", label: "Variants" },
  { tab: "providers", label: "Providers" },
  { tab: "steering", label: "Reflexes & triggers" },
  { tab: "context", label: "Context" },
  { tab: "history", label: "History" },
  { tab: "evals", label: "Evals" },
  { tab: "preview", label: "Assembled prompt" },
]

const STEERING = "workbench/steering.json"
const OPTIONS = "workbench/options.json"

// ---- Small helpers ----

function errText(e: unknown): string {
  if (e instanceof ApiError) {
    const body = e.body as { error?: string } | undefined
    return body?.error ?? e.message
  }
  return e instanceof Error ? e.message : String(e)
}

function reportOf(e: unknown): Report | undefined {
  if (e instanceof ApiError) return (e.body as { report?: Report } | undefined)?.report
  return undefined
}

function when(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })
}

const short = (d?: string) => (d ? d.slice(0, 10) : "")
const hint = "text-xs text-muted-foreground"

/** Splits "---\nfront\n---\nbody" into its two parts. */
function splitFront(text: string): { front: string; body: string } {
  if (!text.startsWith("---\n")) return { front: "", body: text }
  const end = text.indexOf("\n---\n", 3)
  if (end < 0) return { front: "", body: text }
  return { front: text.slice(4, end + 1), body: text.slice(end + 5) }
}

/** A top-level or nested scalar from simple YAML ("runtime.max_steps"); for showing in forms only. */
function yamlValue(yaml: string, key: string): string {
  const parts = key.split(".")
  const lines = yaml.split("\n")
  let indent = 0
  let i = 0
  for (let p = 0; p < parts.length; p++) {
    const want = `${" ".repeat(indent)}${parts[p]}:`
    let found = -1
    for (; i < lines.length; i++) {
      const l = lines[i]
      if (l.startsWith(want)) {
        found = i
        break
      }
      if (p > 0 && l.trim() && !l.startsWith(" ".repeat(indent))) return ""
    }
    if (found < 0) return ""
    const rest = lines[found].slice(want.length).trim()
    if (p === parts.length - 1) return rest.replace(/^["']|["']$/g, "").replace(/\s+#.*$/, "")
    i = found + 1
    const next = lines.slice(i).find((l) => l.trim())
    indent = next ? next.length - next.trimStart().length : indent + 2
  }
  return ""
}

function Section({ title, children, aside }: { title: string; children: ReactNode; aside?: ReactNode }) {
  return (
    <section className="flex flex-col gap-2 border-b border-border px-4 py-3">
      <div className="flex items-center gap-2">
        <h3 className="flex-1 text-sm font-semibold">{title}</h3>
        {aside}
      </div>
      {children}
    </section>
  )
}

function Problems({ report, file, prefix }: { report?: Report; file?: string; prefix?: string }) {
  const list = (report?.problems ?? []).filter((p) =>
    file !== undefined ? p.file === file : prefix !== undefined ? (p.file ?? "").startsWith(prefix) : true,
  )
  if (list.length === 0) return null
  return (
    <ul className="flex flex-col gap-1">
      {list.map((p, i) => (
        <li
          // biome-ignore lint/suspicious/noArrayIndexKey: problems have no id
          key={i}
          className={`rounded-md px-2 py-1 text-xs ${p.severity === "error" ? "bg-destructive/10 text-destructive" : "bg-warning/10"}`}
        >
          {p.file && <span className="font-mono">{p.file}: </span>}
          {p.message}
        </li>
      ))}
    </ul>
  )
}

/** One knob: a label, an input and where it stands today. */
function Knob({
  label,
  value,
  onChange,
  about,
  status,
  type = "number",
}: {
  label: string
  value: string
  onChange: (v: string) => void
  about?: string
  status: "honored" | "recorded"
  type?: string
}) {
  return (
    <Field>
      <FieldLabel className="flex items-center gap-2">
        {label}
        <Badge variant={status === "honored" ? "secondary" : "outline"}>
          {status === "honored" ? "honored today" : "recorded for later"}
        </Badge>
      </FieldLabel>
      <Input type={type} value={value} onChange={(e) => onChange(e.target.value)} />
      {about && <FieldDescription>{about}</FieldDescription>}
    </Field>
  )
}

function DiffView({ lines }: { lines: DiffLine[] }) {
  return (
    <pre className="overflow-auto rounded-md bg-muted/40 py-1 font-mono text-xs leading-5">
      {lines.map((l, i) => (
        <div
          // biome-ignore lint/suspicious/noArrayIndexKey: a line is its position
          key={i}
          className={l.op === "+" ? "bg-success/15 px-2" : l.op === "-" ? "bg-destructive/15 px-2" : "px-2"}
        >
          {l.op}
          {l.text || " "}
        </div>
      ))}
    </pre>
  )
}

function unifiedLines(diff: string): DiffLine[] {
  return diff
    .split("\n")
    .filter((l) => l && !l.startsWith("---") && !l.startsWith("+++"))
    .map((l) => ({
      op: (l[0] === "+" || l[0] === "-" ? l[0] : " ") as DiffLine["op"],
      text: l.startsWith("@@") ? l : l.slice(1),
    }))
}

/** A Markdown editor with a preview, as in Drafts. */
function MarkdownEditor({
  value,
  onChange,
  rows = 14,
}: {
  value: string
  onChange: (v: string) => void
  rows?: number
}) {
  const [preview, setPreview] = useState(false)
  return (
    <div className="flex flex-col gap-1">
      <div className="flex gap-1">
        <Button size="xs" variant={preview ? "ghost" : "secondary"} onClick={() => setPreview(false)}>
          Edit
        </Button>
        <Button size="xs" variant={preview ? "secondary" : "ghost"} onClick={() => setPreview(true)}>
          Preview
        </Button>
      </div>
      {preview ? (
        <article className="rounded-md border border-border px-4 py-2">
          <Markdown size="document">{value}</Markdown>
        </article>
      ) : (
        <div className="rounded-md border border-border" style={{ height: `${rows * 1.6}rem` }}>
          <TextEditor markdown value={value} onChange={(e) => onChange(e.target.value)} />
        </div>
      )}
    </div>
  )
}

function CodeEditor({ value, onChange, rows = 12 }: { value: string; onChange: (v: string) => void; rows?: number }) {
  return (
    <div className="rounded-md border border-border" style={{ height: `${rows * 1.6}rem` }}>
      <TextEditor spellCheck={false} value={value} onChange={(e) => onChange(e.target.value)} />
    </div>
  )
}

// ---- The view ----

function WorkbenchView({ win, desk, api }: ViewProps) {
  const agentID = typeof win.args.agent === "string" ? win.args.agent : ""
  const [agents, setAgents] = useState<Agent[]>([])
  const [root, setRoot] = useState("")
  const [templates, setTemplates] = useState<Template[]>([])
  const [template, setTemplate] = useState("embedded")
  const [newName, setNewName] = useState("")
  const [importPath, setImportPath] = useState("")
  const [notice, setNotice] = useState("")
  const upload = useRef<HTMLInputElement>(null)

  const load = useCallback(() => {
    api.get<{ agents: Agent[]; root: string }>("agents").then(
      (r) => {
        setAgents(r.agents)
        setRoot(r.root)
      },
      (e) => setNotice(errText(e)),
    )
  }, [api])
  useEffect(load, [load])
  useEffect(() => {
    api.get<Template[]>("templates").then(setTemplates, () => {})
  }, [api])
  useEvent("workbench.changed", load)

  const open = (id: string) => {
    void desk.setArgs(win.id, id ? { agent: id } : {})
    desk.report(win.id, { kind: "data", ref: id ? `workbench:${id}` : "workbench:agents" })
  }
  const create = async () => {
    try {
      const a = await api.post<Agent>("agents", { template, name: newName })
      setNewName("")
      open(a.id)
    } catch (e) {
      setNotice(errText(e))
    }
  }
  const importFile = async (f: File) => {
    try {
      const buf = new Uint8Array(await f.arrayBuffer())
      let bin = ""
      for (let i = 0; i < buf.length; i += 0x8000) bin += String.fromCharCode(...buf.subarray(i, i + 0x8000))
      const r = await api.post<{ agent: Agent; notes?: string[] }>("import", { name: f.name, data: btoa(bin) })
      setNotice(`Imported ${r.agent.id}.${r.notes?.length ? ` ${r.notes.join("; ")}` : ""}`)
      open(r.agent.id)
    } catch (e) {
      setNotice(errText(e))
    }
  }
  const importDir = async () => {
    try {
      const r = await api.post<{ agent: Agent; notes?: string[] }>("import-dir", { path: importPath.trim() })
      setNotice(
        `Copied into ${r.agent.id} (the source was not changed).${r.notes?.length ? ` ${r.notes.join("; ")}` : ""}`,
      )
      setImportPath("")
      open(r.agent.id)
    } catch (e) {
      setNotice(errText(e))
    }
  }

  return (
    <div className="@container flex h-full min-h-0">
      <aside className="flex w-64 shrink-0 flex-col border-r border-border">
        <div className="flex items-center gap-2 border-b border-border px-3 py-2">
          <FlaskConical className="size-4" />
          <span className="flex-1 text-sm font-semibold">Agents</span>
        </div>
        <ul className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-auto p-1">
          {agents.map((a) => (
            <li key={a.id}>
              <Item
                size="xs"
                variant={a.id === agentID ? "muted" : "default"}
                render={
                  <a
                    href={`#agent-${a.id}`}
                    onClick={(e) => {
                      e.preventDefault()
                      open(a.id)
                    }}
                  />
                }
              >
                <ItemContent className="min-w-0">
                  <ItemTitle className="w-full truncate">
                    {a.name}
                    {!a.ok && <Badge variant="destructive">invalid</Badge>}
                  </ItemTitle>
                  <ItemDescription>
                    {a.id} · v{a.version} · {short(a.digest)}
                  </ItemDescription>
                </ItemContent>
              </Item>
            </li>
          ))}
          {agents.length === 0 && <li className={`${hint} px-2 py-2`}>No agents yet. Make one below or import one.</li>}
        </ul>
        <div className="flex flex-col gap-1.5 border-t border-border p-2">
          <Select
            value={template}
            onValueChange={(v) => v && setTemplate(String(v))}
            items={templates.map((t) => ({ value: t.name, label: t.name }))}
          >
            <SelectTrigger size="sm" aria-label="Template">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {templates.map((t) => (
                <SelectItem key={t.name} value={t.name}>
                  {t.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <div className="flex gap-1">
            <Input
              placeholder="Name"
              aria-label="New agent's name"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
            />
            <Button size="sm" onClick={() => void create()}>
              <Plus /> New
            </Button>
          </div>
          <Input
            ref={upload}
            className="hidden"
            type="file"
            accept=".tar.gz,.tgz,.zip,application/gzip,application/zip"
            onChange={(e) => {
              const f = e.target.files?.[0]
              if (f) void importFile(f)
              e.target.value = ""
            }}
          />
          <Button size="sm" variant="outline" onClick={() => upload.current?.click()}>
            <Upload /> Import archive
          </Button>
          <div className="flex gap-1">
            <Input
              placeholder="/path/to/agent-dir"
              aria-label="Agent directory to copy"
              value={importPath}
              onChange={(e) => setImportPath(e.target.value)}
            />
            <Button
              size="sm"
              variant="outline"
              aria-label="Copy a directory in"
              disabled={!importPath.trim()}
              onClick={() => void importDir()}
            >
              <FolderInput />
            </Button>
          </div>
          <p className={`${hint} break-all`}>Library: {root}</p>
        </div>
      </aside>
      <main className="flex min-w-0 flex-1 flex-col">
        {notice && (
          <div className="flex items-center gap-2 bg-warning/10 px-3 py-1.5 text-xs">
            <span className="flex-1">{notice}</span>
            <Button variant="ghost" size="xs" onClick={() => setNotice("")}>
              Dismiss
            </Button>
          </div>
        )}
        {agentID ? (
          <AgentEditor key={agentID} id={agentID} win={win} desk={desk} api={api} onGone={() => open("")} />
        ) : (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <FlaskConical />
              </EmptyMedia>
              <EmptyTitle>Agent Workbench</EmptyTitle>
              <EmptyDescription>
                Pick an agent, make one from a template, or import one (an archive, or a directory copied in read-only).
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
      </main>
    </div>
  )
}

// ---- The editor ----

interface EditorProps extends ViewProps {
  id: string
  onGone: () => void
}

function AgentEditor({ id, win, desk, api, onGone }: EditorProps) {
  const [view, setView] = useState<AgentView | null>(null)
  const [files, setFiles] = useState<Files>({})
  const [report, setReport] = useState<Report | undefined>()
  const [note, setNote] = useState("")
  const [notice, setNotice] = useState("")
  const [tab, setTab] = useState<Tab>((win.state?.tab as Tab) || "prompt")

  const load = useCallback(async () => {
    try {
      const v = await api.get<AgentView>(`agents/${id}`)
      setView(v)
      setFiles(v.files)
      setReport(v.report)
      desk.setDirty(win.id, false)
      void desk.setTitle(win.id, `Workbench: ${v.agent.name}`)
    } catch (e) {
      setNotice(errText(e))
    }
  }, [api, id, desk, win.id])
  useEffect(() => {
    void load()
  }, [load])

  const dirty = useMemo(() => {
    if (!view) return false
    const keys = new Set([...Object.keys(view.files), ...Object.keys(files)])
    for (const k of keys) if (view.files[k] !== files[k]) return true
    return false
  }, [view, files])
  useEffect(() => desk.setDirty(win.id, dirty), [dirty, desk, win.id])
  useEffect(() => desk.report(win.id, { kind: "data", ref: `workbench:${id}`, tab }), [desk, win.id, id, tab])

  useEvent<{ agent?: string; version?: number }>("workbench.changed", (ev) => {
    if (ev.data?.agent !== id) return
    if (ev.data?.version && view && ev.data.version !== view.agent.version) {
      if (dirty) setNotice(`Version ${ev.data.version} was saved elsewhere while you were editing.`)
      else void load()
    }
  })

  const changes = (): Record<string, string | null> => {
    const out: Record<string, string | null> = {}
    if (!view) return out
    for (const k of new Set([...Object.keys(view.files), ...Object.keys(files)])) {
      if (view.files[k] !== files[k]) out[k] = files[k] ?? null
    }
    return out
  }

  // Validate the working copy as it changes (no save).
  const timer = useRef<number | undefined>(undefined)
  // biome-ignore lint/correctness/useExhaustiveDependencies: changes() reads files and view; revalidate when the files change
  useEffect(() => {
    if (!view || !dirty) {
      if (view) setReport(view.report)
      return
    }
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => {
      api.put<{ report: Report }>(`agents/${id}/files`, { changes: changes(), dry_run: true }).then(
        (r) => setReport(r.report),
        (e) => setReport(reportOf(e)),
      )
    }, 500)
  }, [files, view, dirty, api, id])

  const setFile = (path: string, content: string | undefined) =>
    setFiles((f) => {
      const next = { ...f }
      if (content === undefined) delete next[path]
      else next[path] = content
      return next
    })

  /** A form edit: the server sets YAML keys (keeping the rest) and returns the files. */
  const formEdit = async (file: string, set: Record<string, unknown>, body?: string) => {
    try {
      const r = await api.put<{ report: Report; files: Files }>(`agents/${id}/files`, {
        changes: changes(),
        edits: [{ file, set, body }],
        dry_run: true,
      })
      setFiles(r.files)
      setReport(r.report)
    } catch (e) {
      setNotice(errText(e))
    }
  }

  const save = async () => {
    if (!view) return
    try {
      await api.put(`agents/${id}/files`, {
        changes: changes(),
        base: view.report.digest,
        note: note || "edited in the Workbench",
      })
      setNote("")
      setNotice("")
      await load()
    } catch (e) {
      const rep = reportOf(e)
      if (rep) setReport(rep)
      setNotice(errText(e))
    }
  }

  const remove = async () => {
    const ok = await desk.confirm({
      title: `Delete ${id}?`,
      message: "It moves to the library's trash; nothing is erased.",
      confirmLabel: "Move to trash",
      danger: true,
    })
    if (!ok) return
    await api.del(`agents/${id}`)
    onGone()
  }
  const duplicate = async () => {
    const a = await api.post<Agent>(`agents/${id}/duplicate`, {})
    void desk.setArgs(win.id, { agent: a.id })
  }

  if (!view) return <p className={`${hint} p-3`}>{notice || "Loading…"}</p>
  const errors = (report?.problems ?? []).filter((p) => p.severity === "error").length
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold">
            {view.agent.name} <span className={hint}>({id})</span>
          </div>
          <div className={`${hint} truncate`} title={report?.digest}>
            v{view.agent.version} · digest {short(report?.digest)} · agentdir {short(report?.agentdir_digest)} ·{" "}
            {report?.summary?.kind}
          </div>
        </div>
        {report?.ok ? <Badge variant="secondary">valid</Badge> : <Badge variant="destructive">{errors} errors</Badge>}
        {dirty && <Badge variant="outline">unsaved</Badge>}
        <Input
          className="w-56"
          placeholder="What changed (version note)"
          aria-label="Version note"
          value={note}
          onChange={(e) => setNote(e.target.value)}
        />
        <Button size="sm" disabled={!dirty} onClick={() => void save()}>
          <Save /> Save
        </Button>
        <Button size="sm" variant="ghost" disabled={!dirty} onClick={() => void load()}>
          <RotateCcw /> Revert
        </Button>
        <Button size="sm" variant="outline" render={<a href={api.url(`agents/${id}/export`)} download />}>
          <Download /> Export
        </Button>
        <Button size="icon-sm" variant="ghost" aria-label="Duplicate" onClick={() => void duplicate()}>
          <Copy />
        </Button>
        <Button size="icon-sm" variant="ghost" aria-label="Delete" onClick={() => void remove()}>
          <Trash2 />
        </Button>
      </header>
      {notice && (
        <div className="flex items-center gap-2 bg-warning/10 px-3 py-1.5 text-xs">
          <span className="flex-1">{notice}</span>
          <Button variant="outline" size="xs" onClick={() => void load()}>
            Reload
          </Button>
        </div>
      )}
      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)} className="border-b border-border px-2 pt-1">
        <TabsList variant="line" className="flex-wrap">
          {TABS.map((t) => (
            <TabsTrigger key={t.tab} value={t.tab}>
              {t.label}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <div className="min-h-0 flex-1 overflow-auto">
        {tab === "prompt" && (
          <PromptTab files={files} setFile={setFile} formEdit={formEdit} report={report} api={api} id={id} />
        )}
        {tab === "skills" && (
          <MarkdownFilesTab kind="skills" files={files} setFile={setFile} formEdit={formEdit} report={report} />
        )}
        {tab === "tools" && <ToolsTab files={files} setFile={setFile} report={report} />}
        {tab === "variants" && (
          <MarkdownFilesTab kind="variants" files={files} setFile={setFile} formEdit={formEdit} report={report} />
        )}
        {tab === "providers" && <ProvidersTab files={files} setFile={setFile} report={report} />}
        {tab === "steering" && <SteeringTab files={files} setFile={setFile} report={report} />}
        {tab === "context" && <ContextTab files={files} setFile={setFile} formEdit={formEdit} report={report} />}
        {tab === "history" && <HistoryTab view={view} api={api} id={id} desk={desk} reload={load} />}
        {tab === "evals" && <EvalsTab api={api} id={id} desk={desk} digest={view.report.digest} dirty={dirty} />}
        {tab === "preview" && <PreviewTab api={api} id={id} version={view.agent.version} />}
      </div>
    </div>
  )
}

interface FileProps {
  files: Files
  setFile: (path: string, content: string | undefined) => void
  report?: Report
}
interface FormProps extends FileProps {
  formEdit: (file: string, set: Record<string, unknown>, body?: string) => Promise<void>
}

// ---- Prompt ----

function PromptTab({ files, setFile, formEdit, report, api, id }: FormProps & { api: ModuleAPI; id: string }) {
  const md = files["agent.md"] ?? ""
  const { front, body } = splitFront(md)
  const [raw, setRaw] = useState(false)
  const [fields, setFields] = useState({
    name: yamlValue(front, "name"),
    description: yamlValue(front, "description"),
    purpose: yamlValue(front, "behavior.purpose"),
  })
  const [base, setBase] = useState<PreviewSlot | null>(null)
  useEffect(() => {
    api.get<Preview>(`agents/${id}/preview`).then(
      (p) => setBase(p.slots.find((s) => s.name === "universal") ?? null),
      () => {},
    )
  }, [api, id])
  const apply = () =>
    void formEdit("agent.md", {
      name: fields.name,
      description: fields.description,
      "behavior.purpose": fields.purpose,
    })
  return (
    <>
      <Section title="The agent (agent.md front matter)" aside={<Problems report={report} file="agent.md" />}>
        <div className="grid grid-cols-1 gap-2 @3xl:grid-cols-3">
          <Field>
            <FieldLabel>Name</FieldLabel>
            <Input
              value={fields.name}
              onChange={(e) => setFields({ ...fields, name: e.target.value })}
              onBlur={apply}
            />
          </Field>
          <Field>
            <FieldLabel>Description</FieldLabel>
            <Input
              value={fields.description}
              onChange={(e) => setFields({ ...fields, description: e.target.value })}
              onBlur={apply}
            />
          </Field>
          <Field>
            <FieldLabel>Purpose (behavior.purpose)</FieldLabel>
            <Input
              value={fields.purpose}
              onChange={(e) => setFields({ ...fields, purpose: e.target.value })}
              onBlur={apply}
            />
          </Field>
        </div>
        <Button size="xs" variant="ghost" className="self-start" onClick={() => setRaw(!raw)}>
          {raw ? "Hide" : "Show"} the whole file
        </Button>
        {raw && <CodeEditor value={md} onChange={(v) => setFile("agent.md", v)} rows={16} />}
      </Section>
      <Section title="Agent prompt (agent.md body: Nanite's Agent slot)">
        <MarkdownEditor
          value={body}
          onChange={(v) => setFile("agent.md", front ? `---\n${front}---\n${v}` : v)}
          rows={16}
        />
        <p className={hint}>
          The agent's prompt is this body plus the context files (Context tab); a subtask variant's prompt is its own
          body (Variants tab).
        </p>
      </Section>
      <Section title="Base prompt (Nanite's Universal slot: engine-owned, read-only)">
        <Collapsible>
          <CollapsibleTrigger render={<Button size="xs" variant="ghost" className="self-start" />}>
            Show the base prompt
          </CollapsibleTrigger>
          <CollapsibleContent>
            <pre className="max-h-96 overflow-auto rounded-md bg-muted/40 p-2 text-xs whitespace-pre-wrap">
              {base?.content ?? "(not available)"}
            </pre>
          </CollapsibleContent>
        </Collapsible>
        <p className={hint}>
          Boot prompts and boot profiles are Tether catalog concepts, not part of an agent directory; the Workbench does
          not edit them.
        </p>
      </Section>
    </>
  )
}

// ---- Skills and variants ----

const VARIANT_KNOBS: { key: string; label: string; type?: string; about?: string }[] = [
  { key: "description", label: "Description", type: "text", about: "Shown to the model in the subtask tool's list." },
  { key: "last_n", label: "last_n", about: "Parent messages the child sees by default (1–50; empty: 6)." },
  { key: "result_cap", label: "result_cap", about: "Result size cap in tokens (clamped to about 5,900)." },
  { key: "budgets.steps", label: "budgets.steps" },
  { key: "budgets.tokens", label: "budgets.tokens" },
  { key: "budgets.wall", label: "budgets.wall", type: "text", about: "A duration such as 10m." },
  { key: "model", label: "model", type: "text" },
  { key: "provider", label: "provider", type: "text", about: "Needs a model." },
]

function MarkdownFilesTab({ kind, files, setFile, formEdit, report }: FormProps & { kind: "skills" | "variants" }) {
  const pathOf = (name: string) => (kind === "skills" ? `skills/${name}/SKILL.md` : `variants/${name}.md`)
  const names = Object.keys(files)
    .filter((p) => (kind === "skills" ? /^skills\/[^/]+\/SKILL\.md$/.test(p) : /^variants\/[^/]+\.md$/.test(p)))
    .map((p) => (kind === "skills" ? p.split("/")[1] : p.slice(9, -3)))
    .sort()
  const [picked, setPicked] = useState(names[0] ?? "")
  const [newName, setNewName] = useState("")
  const path = picked ? pathOf(picked) : ""
  const text = path ? (files[path] ?? "") : ""
  const { front, body } = splitFront(text)
  const [knobs, setKnobs] = useState<Record<string, string>>({})
  // biome-ignore lint/correctness/useExhaustiveDependencies: refill the form only when another file is picked, not on each keystroke
  useEffect(() => {
    const k: Record<string, string> = {}
    for (const f of kind === "skills" ? [{ key: "name" }, { key: "description" }] : VARIANT_KNOBS)
      k[f.key] = yamlValue(front, f.key)
    k.read_only = yamlValue(front, "read_only")
    k.can_spawn = yamlValue(front, "can_spawn")
    setKnobs(k)
  }, [picked, kind])

  const add = () => {
    const n = newName.trim().toLowerCase()
    if (!n) return
    const body =
      kind === "skills"
        ? `---\nname: ${n}\ndescription: "Say when to use this skill."\n---\nThe skill's instructions.\n`
        : `---\ndescription: "Say what this subtask is for."\nlast_n: 4\n---\nYou are a ${n} subtask. Say what to do and what to return.\n`
    setFile(pathOf(n), body)
    setPicked(n)
    setNewName("")
  }
  const numeric = (v: string) => (v.trim() === "" ? null : /^-?\d+(\.\d+)?$/.test(v.trim()) ? Number(v) : v)
  const applyKnobs = (next: Record<string, string>) => {
    const set: Record<string, unknown> = {}
    if (kind === "skills") {
      set.name = next.name
      set.description = next.description
    } else {
      for (const f of VARIANT_KNOBS) set[f.key] = f.type === "text" ? next[f.key] : numeric(next[f.key] ?? "")
      set.read_only = next.read_only === "true"
      set.can_spawn = next.can_spawn === "true"
    }
    void formEdit(path, set)
  }

  return (
    <div className="flex min-h-0">
      <div className="flex w-52 shrink-0 flex-col gap-1 border-r border-border p-2">
        {names.map((n) => (
          <Button
            key={n}
            size="sm"
            variant={n === picked ? "secondary" : "ghost"}
            className="justify-start"
            onClick={() => setPicked(n)}
          >
            {n}
          </Button>
        ))}
        {names.length === 0 && <p className={hint}>None yet.</p>}
        <div className="mt-2 flex gap-1">
          <Input
            placeholder={kind === "skills" ? "skill-name" : "variant-name"}
            aria-label="New name"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
          />
          <Button size="icon-sm" aria-label="Add" onClick={add}>
            <Plus />
          </Button>
        </div>
        <p className={hint}>
          {kind === "skills"
            ? "skills/<name>/SKILL.md: front matter name and description; the body is fetched on demand (skill_get)."
            : "variants/<name>.md: a kind of subtask the agent can start with the subtask tool (Nanite Code Mode)."}
        </p>
      </div>
      <div className="min-w-0 flex-1">
        {path ? (
          <>
            <Section
              title={path}
              aside={
                <Button size="xs" variant="ghost" onClick={() => setFile(path, undefined)}>
                  <Trash2 /> Remove
                </Button>
              }
            >
              <Problems report={report} file={path} />
              <div className="grid grid-cols-1 gap-2 @3xl:grid-cols-2">
                {(kind === "skills"
                  ? [
                      { key: "name", label: "Name", type: "text" },
                      {
                        key: "description",
                        label: "Description",
                        type: "text",
                        about: "The model chooses skills by it.",
                      },
                    ]
                  : VARIANT_KNOBS
                ).map((f) => (
                  <Knob
                    key={f.key}
                    label={f.label}
                    type={f.type ?? "number"}
                    value={knobs[f.key] ?? ""}
                    status="honored"
                    about={"about" in f ? (f.about as string) : undefined}
                    onChange={(v) => setKnobs({ ...knobs, [f.key]: v })}
                  />
                ))}
                {kind === "variants" && (
                  <div className="flex items-center gap-4">
                    <FieldLabel className="flex items-center gap-2">
                      <Switch
                        checked={knobs.read_only === "true"}
                        onCheckedChange={(c) => applyKnobs({ ...knobs, read_only: String(c) })}
                      />
                      read_only
                    </FieldLabel>
                    <FieldLabel className="flex items-center gap-2">
                      <Switch
                        checked={knobs.can_spawn === "true"}
                        onCheckedChange={(c) => applyKnobs({ ...knobs, can_spawn: String(c) })}
                      />
                      can_spawn (a lead)
                    </FieldLabel>
                  </div>
                )}
              </div>
              <Button size="xs" variant="outline" className="self-start" onClick={() => applyKnobs(knobs)}>
                Apply the fields
              </Button>
            </Section>
            <Section title={kind === "skills" ? "Instructions" : "Role prompt"}>
              <MarkdownEditor value={body} onChange={(v) => setFile(path, front ? `---\n${front}---\n${v}` : v)} />
              {kind === "variants" && (
                <p className={hint}>
                  Tool allow and deny lists: edit them in the front matter below (tools.allow, tools.deny).
                </p>
              )}
              <Collapsible>
                <CollapsibleTrigger render={<Button size="xs" variant="ghost" />}>The whole file</CollapsibleTrigger>
                <CollapsibleContent>
                  <CodeEditor value={text} onChange={(v) => setFile(path, v)} />
                </CollapsibleContent>
              </Collapsible>
            </Section>
          </>
        ) : (
          <p className={`${hint} p-4`}>Add one on the left.</p>
        )}
      </div>
    </div>
  )
}

// ---- Tools ----

interface MCPServer {
  type?: string
  command?: string
  args?: string[]
  env?: Record<string, string>
  url?: string
  headers?: Record<string, string>
}

const kv = (m?: Record<string, string>) =>
  Object.entries(m ?? {})
    .map(([k, v]) => `${k}=${v}`)
    .join("\n")
const unkv = (s: string) => {
  const out: Record<string, string> = {}
  for (const line of s.split("\n")) {
    const i = line.indexOf("=")
    if (i > 0) out[line.slice(0, i).trim()] = line.slice(i + 1).trim()
  }
  return Object.keys(out).length ? out : undefined
}

function ToolsTab({ files, setFile, report }: FileProps) {
  const raw = files[".mcp.json"] ?? ""
  let servers: Record<string, MCPServer> = {}
  let parseError = ""
  try {
    servers = raw.trim() ? ((JSON.parse(raw) as { mcpServers?: Record<string, MCPServer> }).mcpServers ?? {}) : {}
  } catch (e) {
    parseError = e instanceof Error ? e.message : String(e)
  }
  const write = (next: Record<string, MCPServer>) =>
    setFile(".mcp.json", `${JSON.stringify({ mcpServers: next }, null, 2)}\n`)
  const [newName, setNewName] = useState("")
  const update = (name: string, s: MCPServer) => write({ ...servers, [name]: s })
  return (
    <>
      <Section title="MCP servers (.mcp.json)" aside={<Problems report={report} file=".mcp.json" />}>
        {parseError && <p className="text-xs text-destructive">.mcp.json is not JSON: {parseError}</p>}
        {Object.entries(servers).map(([name, s]) => {
          const http = s.type === "http" || s.type === "sse"
          return (
            <div key={name} className="flex flex-col gap-2 rounded-md border border-border p-2">
              <div className="flex items-center gap-2">
                <span className="flex-1 font-mono text-sm">{name}</span>
                <Select
                  value={s.type || "stdio"}
                  onValueChange={(v) =>
                    update(
                      name,
                      v === "stdio"
                        ? { command: s.command ?? "", args: s.args, env: s.env }
                        : { type: String(v), url: s.url ?? "https://", headers: s.headers },
                    )
                  }
                  items={["stdio", "http", "sse"].map((v) => ({ value: v, label: v }))}
                >
                  <SelectTrigger size="sm" aria-label="Transport">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {["stdio", "http", "sse"].map((v) => (
                      <SelectItem key={v} value={v}>
                        {v}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button
                  size="xs"
                  variant="ghost"
                  onClick={() => {
                    const next = { ...servers }
                    delete next[name]
                    write(next)
                  }}
                >
                  <Trash2 />
                </Button>
              </div>
              {http ? (
                <>
                  <Input
                    aria-label="URL"
                    value={s.url ?? ""}
                    onChange={(e) => update(name, { ...s, url: e.target.value })}
                  />
                  <Textarea
                    aria-label="Headers"
                    placeholder="Header=${VARIABLE} (one a line)"
                    value={kv(s.headers)}
                    onChange={(e) => update(name, { ...s, headers: unkv(e.target.value) })}
                  />
                </>
              ) : (
                <>
                  <Input
                    aria-label="Command"
                    placeholder="command"
                    value={s.command ?? ""}
                    onChange={(e) => update(name, { ...s, command: e.target.value })}
                  />
                  <Input
                    aria-label="Arguments"
                    placeholder="arguments, separated by spaces"
                    value={(s.args ?? []).join(" ")}
                    onChange={(e) => update(name, { ...s, args: e.target.value.split(" ").filter(Boolean) })}
                  />
                  <Textarea
                    aria-label="Environment"
                    placeholder="NAME=${VARIABLE} (one a line)"
                    value={kv(s.env)}
                    onChange={(e) => update(name, { ...s, env: unkv(e.target.value) })}
                  />
                </>
              )}
            </div>
          )
        })}
        <div className="flex gap-1">
          <Input
            placeholder="server-name"
            aria-label="New server"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
          />
          <Button
            size="sm"
            disabled={!newName.trim() || !!parseError}
            onClick={() => {
              update(newName.trim(), { command: "" })
              setNewName("")
            }}
          >
            <Plus /> Server
          </Button>
        </div>
        <p className={hint}>
          Credentials never go in this file: use ${"{"}VARIABLE{"}"} or a secret reference (keychain://, file://,
          env://). Tools appear as mcp__&lt;server&gt;__&lt;tool&gt;. Eval runs start no MCP server.
        </p>
        <Collapsible>
          <CollapsibleTrigger render={<Button size="xs" variant="ghost" />}>Raw JSON</CollapsibleTrigger>
          <CollapsibleContent>
            <CodeEditor value={raw} onChange={(v) => setFile(".mcp.json", v)} />
          </CollapsibleContent>
        </Collapsible>
      </Section>
      <Section title="Tool permissions (permissions.yaml)" aside={<Problems report={report} file="permissions.yaml" />}>
        <CodeEditor
          value={files["permissions.yaml"] ?? ""}
          onChange={(v) => setFile("permissions.yaml", v.trim() ? v : undefined)}
          rows={8}
        />
        <p className={hint}>
          permissions: mode (default, accept-edits, plan, yolo) and rules (tool, pattern, behavior allow / ask / deny).
        </p>
      </Section>
    </>
  )
}

// ---- Providers ----

const PROVIDERS_EXAMPLE = `providers:
  local:
    wire: openai-chat
    base_url: http://127.0.0.1:8080/v1
    auth: none
use:
  summarizer: {provider: anthropic, model: claude-haiku-5-5}
`

function ProvidersTab({ files, setFile, report }: FileProps) {
  const raw = files["providers.yaml"]
  return (
    <Section title="Named providers (providers.yaml)" aside={<Problems report={report} file="providers.yaml" />}>
      {raw === undefined ? (
        <Button
          size="sm"
          variant="outline"
          className="self-start"
          onClick={() => setFile("providers.yaml", PROVIDERS_EXAMPLE)}
        >
          <Plus /> Add providers.yaml
        </Button>
      ) : (
        <>
          <CodeEditor value={raw} onChange={(v) => setFile("providers.yaml", v)} rows={14} />
          <Button size="xs" variant="ghost" className="self-start" onClick={() => setFile("providers.yaml", undefined)}>
            <Trash2 /> Remove the file
          </Button>
        </>
      )}
      <p className={hint}>
        Providers by name (wire anthropic, openai, openai-chat, openai-responses; base_url; key_ref, never a key) and a
        provider and model per use: chat, summarizer (compaction), subtask. Checked with Nanite's own ProviderSpec
        rules. Eval runs send every request through the app's gateway, so a use here does not change where an eval run
        goes.
      </p>
    </Section>
  )
}

// ---- Reflexes and triggers (steering) ----

interface Reflex {
  name: string
  about?: string
  trigger_kind: string
  trigger: Record<string, unknown>
  action_kind: string
  action: Record<string, unknown>
  cooldown_seconds?: number
  enabled?: boolean
}
interface Preset {
  name: string
  about?: string
  instructions?: string
  slots?: string[]
  reflexes?: string[]
  tools?: string[]
  dispatch?: string[]
  output_style?: string
  delivery?: string
  rules?: string[]
}
interface Steering {
  workbench_format: number
  presets?: Preset[]
  reflexes?: Reflex[]
}

const TRIGGER_FIELD: Record<string, string> = { text: "pattern", event: "topic", focus: "view" }
const ACTION_FIELD: Record<string, string> = {
  remind: "body",
  halt: "message",
  run_command: "command",
  call_tool: "tool",
}
const list = (v?: string[]) => (v ?? []).join(", ")
const unlist = (s: string) => {
  const out = s
    .split(",")
    .map((x) => x.trim())
    .filter(Boolean)
  return out.length ? out : undefined
}

function KindSelect({
  value,
  options,
  onChange,
  label,
}: {
  value: string
  options: string[]
  onChange: (v: string) => void
  label: string
}) {
  return (
    <Select
      value={value}
      onValueChange={(v) => v && onChange(String(v))}
      items={options.map((o) => ({ value: o, label: o }))}
    >
      <SelectTrigger size="sm" aria-label={label}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {options.map((o) => (
          <SelectItem key={o} value={o}>
            {o}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function SteeringTab({ files, setFile, report }: FileProps) {
  const raw = files[STEERING]
  let s: Steering = { workbench_format: 1 }
  let parseError = ""
  try {
    if (raw?.trim()) s = JSON.parse(raw) as Steering
  } catch (e) {
    parseError = e instanceof Error ? e.message : String(e)
  }
  const write = (next: Steering) => setFile(STEERING, `${JSON.stringify(next, null, 2)}\n`)
  const reflexes = s.reflexes ?? []
  const presets = s.presets ?? []
  const setReflex = (i: number, r: Reflex) => write({ ...s, reflexes: reflexes.map((x, k) => (k === i ? r : x)) })
  const setPreset = (i: number, p: Preset) => write({ ...s, presets: presets.map((x, k) => (k === i ? p : x)) })
  return (
    <>
      <div className="mx-4 mt-3 rounded-md bg-muted/50 px-3 py-2 text-xs">
        <strong>Steering is applied in eval runs.</strong> Reflexes and presets are saved in workbench/steering.json and
        included in exports. Reminder and halt actions run during evals; Surface command and tool actions require a
        connected host.
      </div>
      <Section title="Reflexes" aside={<Problems report={report} file={STEERING} />}>
        {parseError && <p className="text-xs text-destructive">steering.json is not JSON: {parseError}</p>}
        {reflexes.map((r, i) => {
          const tf = TRIGGER_FIELD[r.trigger_kind]
          const af = ACTION_FIELD[r.action_kind]
          return (
            <div
              // biome-ignore lint/suspicious/noArrayIndexKey: reflexes are edited in place by position
              key={i}
              className="grid grid-cols-1 gap-2 rounded-md border border-border p-2 @3xl:grid-cols-4"
            >
              <Input aria-label="Name" value={r.name} onChange={(e) => setReflex(i, { ...r, name: e.target.value })} />
              <Input
                aria-label="About"
                placeholder="what it is for"
                value={r.about ?? ""}
                onChange={(e) => setReflex(i, { ...r, about: e.target.value || undefined })}
              />
              <FieldLabel className="flex items-center gap-2">
                <Switch
                  checked={!!r.enabled}
                  onCheckedChange={(c) => setReflex(i, { ...r, enabled: c || undefined })}
                />
                on everywhere
              </FieldLabel>
              <Button
                size="xs"
                variant="ghost"
                className="justify-self-end"
                onClick={() => write({ ...s, reflexes: reflexes.filter((_, k) => k !== i) })}
              >
                <Trash2 />
              </Button>
              <KindSelect
                label="Trigger kind"
                value={r.trigger_kind}
                options={["text", "predicate", "event", "focus"]}
                onChange={(v) =>
                  setReflex(i, {
                    ...r,
                    trigger_kind: v,
                    trigger: v === "predicate" ? { kind: "tool_calls_window", window: 3, op: "=", value: 0 } : {},
                  })
                }
              />
              {tf ? (
                <Input
                  className="@3xl:col-span-3"
                  aria-label="Trigger"
                  placeholder={tf}
                  value={String(r.trigger[tf] ?? "")}
                  onChange={(e) => setReflex(i, { ...r, trigger: { [tf]: e.target.value } })}
                />
              ) : (
                <Textarea
                  className="font-mono @3xl:col-span-3"
                  aria-label="Predicate"
                  defaultValue={JSON.stringify(r.trigger)}
                  onBlur={(e) => {
                    try {
                      setReflex(i, { ...r, trigger: JSON.parse(e.target.value) })
                    } catch {
                      /* kept as typed; validation reports it */
                    }
                  }}
                />
              )}
              <KindSelect
                label="Action kind"
                value={r.action_kind}
                options={["remind", "halt", "run_command", "call_tool"]}
                onChange={(v) => setReflex(i, { ...r, action_kind: v, action: {} })}
              />
              <Input
                className="@3xl:col-span-2"
                aria-label="Action"
                placeholder={af}
                value={String(r.action[af] ?? "")}
                onChange={(e) => setReflex(i, { ...r, action: { ...r.action, [af]: e.target.value } })}
              />
              <Input
                type="number"
                aria-label="Cooldown"
                placeholder="cooldown seconds"
                value={r.cooldown_seconds ?? ""}
                onChange={(e) => setReflex(i, { ...r, cooldown_seconds: Number(e.target.value) || undefined })}
              />
            </div>
          )
        })}
        <Button
          size="sm"
          variant="outline"
          className="self-start"
          disabled={!!parseError}
          onClick={() =>
            write({
              ...s,
              reflexes: [
                ...reflexes,
                {
                  name: `reflex-${reflexes.length + 1}`,
                  trigger_kind: "text",
                  trigger: { pattern: "(?i)example" },
                  action_kind: "remind",
                  action: { body: "Say what to remember." },
                },
              ],
            })
          }
        >
          <Plus /> Reflex
        </Button>
        <p className={hint}>
          Triggers: text (a regular expression over the owner's message), predicate (libs agent/core reflexes, e.g.{" "}
          {'{"kind":"input_tokens_window","window":2,"op":">","value":100000}'}), event (a topic), focus (a view).
          Actions: remind and halt (steer's own), run_command and call_tool (Surface's). Halt answers a message, so only
          text and predicate triggers may halt.
        </p>
      </Section>
      <Section title="Presets (modes the owner can choose)">
        {presets.map((p, i) => (
          <div
            // biome-ignore lint/suspicious/noArrayIndexKey: presets are edited in place by position
            key={i}
            className="grid grid-cols-1 gap-2 rounded-md border border-border p-2 @3xl:grid-cols-3"
          >
            <Input
              aria-label="Preset name"
              value={p.name}
              onChange={(e) => setPreset(i, { ...p, name: e.target.value })}
            />
            <Input
              aria-label="About"
              placeholder="about"
              value={p.about ?? ""}
              onChange={(e) => setPreset(i, { ...p, about: e.target.value || undefined })}
            />
            <Button
              size="xs"
              variant="ghost"
              className="justify-self-end"
              onClick={() => write({ ...s, presets: presets.filter((_, k) => k !== i) })}
            >
              <Trash2 />
            </Button>
            <Textarea
              className="@3xl:col-span-3"
              aria-label="Instructions"
              placeholder="standing instructions while in force"
              value={p.instructions ?? ""}
              onChange={(e) => setPreset(i, { ...p, instructions: e.target.value || undefined })}
            />
            <Input
              aria-label="Reflexes"
              placeholder="reflexes, comma separated"
              value={list(p.reflexes)}
              onChange={(e) => setPreset(i, { ...p, reflexes: unlist(e.target.value) })}
            />
            <Input
              aria-label="Slots"
              placeholder="slots"
              value={list(p.slots)}
              onChange={(e) => setPreset(i, { ...p, slots: unlist(e.target.value) })}
            />
            <Input
              aria-label="Dispatch"
              placeholder="dispatch (subtask, council, seat)"
              value={list(p.dispatch)}
              onChange={(e) => setPreset(i, { ...p, dispatch: unlist(e.target.value) })}
            />
          </div>
        ))}
        <Button
          size="sm"
          variant="outline"
          className="self-start"
          disabled={!!parseError}
          onClick={() => write({ ...s, presets: [...presets, { name: `preset-${presets.length + 1}` }] })}
        >
          <Plus /> Preset
        </Button>
        <p className={hint}>Only the owner sets a mode; no agent tool does.</p>
        <Collapsible>
          <CollapsibleTrigger render={<Button size="xs" variant="ghost" />}>Raw JSON</CollapsibleTrigger>
          <CollapsibleContent>
            <CodeEditor value={raw ?? ""} onChange={(v) => setFile(STEERING, v.trim() ? v : undefined)} />
          </CollapsibleContent>
        </Collapsible>
      </Section>
    </>
  )
}

// ---- Context ----

const RUNTIME_KNOBS: { key: string; label: string; about: string; type?: string }[] = [
  { key: "runtime.model", label: "model", type: "text", about: "Empty: the app's model (NANITE_DEFAULT_MODEL)." },
  { key: "runtime.provider", label: "provider", type: "text", about: "anthropic (default) or openai." },
  { key: "runtime.max_output_tokens", label: "max_output_tokens", about: "Per model response." },
  { key: "runtime.max_steps", label: "max_steps", about: "Model steps per turn (default 50)." },
  {
    key: "runtime.turn_token_budget",
    label: "turn_token_budget",
    about: "Tokens a turn shares with its subtasks (0: none).",
  },
  {
    key: "runtime.tool_concurrency",
    label: "tool_concurrency",
    about: "Read-only calls and subtasks run side by side (default 4).",
  },
  { key: "runtime.subtask_limit", label: "subtask_limit", about: "Subtasks at once, per pool (default 4, at most 8)." },
  { key: "runtime.subtask_result_cap", label: "subtask_result_cap", about: "Tokens; clamped to about 5,900." },
]

function ContextTab({ files, setFile, formEdit, report }: FormProps) {
  const host = files["host.yaml"] ?? ""
  const [knobs, setKnobs] = useState<Record<string, string>>(() =>
    Object.fromEntries(RUNTIME_KNOBS.map((k) => [k.key, yamlValue(host, k.key)])),
  )
  let options: { workbench_format: number; compact_at?: number } = { workbench_format: 1 }
  try {
    if (files[OPTIONS]?.trim()) options = JSON.parse(files[OPTIONS])
  } catch {
    /* validation reports it */
  }
  const writeOptions = (o: typeof options) => setFile(OPTIONS, `${JSON.stringify(o, null, 2)}\n`)
  const ctxNames = Object.keys(files)
    .filter((p) => /^context\/[^/]+\.md$/.test(p))
    .sort()
  const [picked, setPicked] = useState(ctxNames[0] ?? "")
  const [newName, setNewName] = useState("")
  const apply = () => {
    const set: Record<string, unknown> = {}
    for (const k of RUNTIME_KNOBS) {
      const v = (knobs[k.key] ?? "").trim()
      set[k.key] = v === "" ? null : k.type === "text" ? v : Number(v)
    }
    void formEdit("host.yaml", set)
  }
  return (
    <>
      <Section
        title="Runtime (host.yaml)"
        aside={
          <Button size="xs" variant="outline" onClick={apply}>
            Apply to host.yaml
          </Button>
        }
      >
        <Problems report={report} file="host.yaml" />
        <div className="grid grid-cols-1 gap-2 @3xl:grid-cols-3">
          {RUNTIME_KNOBS.map((k) => (
            <Knob
              key={k.key}
              label={k.label}
              type={k.type ?? "number"}
              value={knobs[k.key] ?? ""}
              about={k.about}
              status="honored"
              onChange={(v) => setKnobs({ ...knobs, [k.key]: v })}
            />
          ))}
        </div>
      </Section>
      <Section
        title="Runtime options (not in the agent dir yet: workbench/options.json)"
        aside={<Problems report={report} file={OPTIONS} />}
      >
        <div className="grid grid-cols-1 gap-2 @3xl:grid-cols-2">
          <Knob
            label="compact_at"
            value={String(options.compact_at ?? "")}
            status="honored"
            about="agent.Options.CompactAt: the request size in tokens (system, tools and conversation) at which Nanite compacts; empty is 80% of the model's context window. Nanite takes it only in code, so the Workbench passes it when it runs the agent (eval runs); a host that opens the agent passes it the same way."
            onChange={(v) => writeOptions({ ...options, compact_at: Number(v) || undefined })}
          />
        </div>
        <p className={hint}>
          Slot budgets and placement are the engine's own and are not set here; the context window comes from the model.
          Variant knobs (last_n, result_cap, budgets) are on the Variants tab; per-use providers (the summarizer used
          for compaction) on the Providers tab.
        </p>
      </Section>
      <div className="flex min-h-0">
        <div className="flex w-52 shrink-0 flex-col gap-1 border-r border-border p-2">
          <span className="text-xs font-semibold">context/*.md</span>
          {ctxNames.map((p) => (
            <Button
              key={p}
              size="sm"
              variant={p === picked ? "secondary" : "ghost"}
              className="justify-start"
              onClick={() => setPicked(p)}
            >
              {p.slice(8)}
            </Button>
          ))}
          <div className="mt-2 flex gap-1">
            <Input
              placeholder="name.md"
              aria-label="New context file"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
            />
            <Button
              size="icon-sm"
              aria-label="Add"
              onClick={() => {
                const n = newName.trim().replace(/\.md$/, "")
                if (!n) return
                setFile(`context/${n}.md`, `# ${n}\n`)
                setPicked(`context/${n}.md`)
                setNewName("")
              }}
            >
              <Plus />
            </Button>
          </div>
          <p className={hint}>Each file is added to Nanite's Context slot, every turn.</p>
        </div>
        <div className="min-w-0 flex-1 p-3">
          {picked && files[picked] !== undefined ? (
            <>
              <div className="mb-1 flex items-center gap-2">
                <span className="flex-1 font-mono text-xs">{picked}</span>
                <Button size="xs" variant="ghost" onClick={() => setFile(picked, undefined)}>
                  <Trash2 /> Remove
                </Button>
              </div>
              <MarkdownEditor value={files[picked]} onChange={(v) => setFile(picked, v)} />
            </>
          ) : (
            <p className={hint}>No context file chosen.</p>
          )}
        </div>
      </div>
    </>
  )
}

// ---- History ----

function HistoryTab({
  view,
  api,
  id,
  desk,
  reload,
}: {
  view: AgentView
  api: ModuleAPI
  id: string
  desk: ViewProps["desk"]
  reload: () => Promise<void>
}) {
  const [picked, setPicked] = useState<number>(0)
  const [diff, setDiff] = useState<FileDiff[]>([])
  useEffect(() => {
    if (!picked) return
    api.get<FileDiff[]>(`agents/${id}/versions/${picked}/diff?to=current`).then(setDiff, () => setDiff([]))
  }, [api, id, picked])
  const rollback = async (n: number) => {
    const ok = await desk.confirm({
      title: `Roll back to version ${n}?`,
      message: "Its files are saved as a new version; nothing is lost.",
      confirmLabel: "Roll back",
    })
    if (!ok) return
    await api.post(`agents/${id}/versions/${n}/rollback`, {})
    await reload()
  }
  return (
    <div className="flex min-h-0">
      <ul className="flex w-80 shrink-0 flex-col gap-0.5 border-r border-border p-1">
        {[...view.versions].reverse().map((v) => (
          <li key={v.n}>
            <Item
              size="xs"
              variant={v.n === picked ? "muted" : "default"}
              render={
                <a
                  href={`#v${v.n}`}
                  onClick={(e) => {
                    e.preventDefault()
                    setPicked(v.n)
                  }}
                />
              }
            >
              <ItemContent className="min-w-0">
                <ItemTitle className="w-full truncate">
                  v{v.n} · {v.note || "(no note)"}
                </ItemTitle>
                <ItemDescription>
                  {v.author} · {when(v.created)} · {short(v.digest)}
                </ItemDescription>
              </ItemContent>
            </Item>
          </li>
        ))}
      </ul>
      <div className="min-w-0 flex-1 p-3">
        {picked ? (
          <>
            <div className="mb-2 flex items-center gap-2">
              <span className="flex-1 text-sm">
                Version {picked} → the current files ({diff.length} files differ)
              </span>
              {picked !== view.agent.version && (
                <Button size="sm" variant="outline" onClick={() => void rollback(picked)}>
                  <RotateCcw /> Roll back to v{picked}
                </Button>
              )}
            </div>
            {diff.map((d) => (
              <div key={d.file} className="mb-3">
                <div className="mb-1 font-mono text-xs">
                  {d.file} <Badge variant="outline">{d.change}</Badge>
                </div>
                <DiffView lines={d.lines} />
              </div>
            ))}
          </>
        ) : (
          <p className={hint}>Pick a version to compare with the current files.</p>
        )}
      </div>
    </div>
  )
}

// ---- Evals ----

const SUITE_EXAMPLE = `name: new-suite
description: What this suite checks.
mode: direct
cases:
  - id: first-case
    turns:
      - "The owner's message."
    fixtures:
      notes/example.md: "A file the agent can read."
    stubs:
      - name: lookup_note
        description: Read a note from the test library.
        read_only: true
        schema: {type: object, properties: {id: {type: string}}}
        results:
          - when: {id: example}
            text: "something"
          - text: "No matching note."
            is_error: true
    graders:
      - contains: "something"
      - max_steps: 6
`

function EvalsTab({
  api,
  id,
  desk,
  digest,
  dirty,
}: {
  api: ModuleAPI
  id: string
  desk: ViewProps["desk"]
  digest: string
  dirty: boolean
}) {
  const [suites, setSuites] = useState<SuiteInfo[]>([])
  const [suite, setSuite] = useState("")
  const [source, setSource] = useState("")
  const [runs, setRuns] = useState<RunInfo[]>([])
  const [run, setRun] = useState<Run | null>(null)
  const [cmp, setCmp] = useState<Comparison | null>(null)
  const [proposals, setProposals] = useState<Proposal[]>([])
  const [estimate, setEstimate] = useState<Estimate | null>(null)
  const [notice, setNotice] = useState("")
  const [busy, setBusy] = useState("")

  const loadSuites = useCallback(() => {
    api.get<SuiteInfo[]>(`agents/${id}/suites`).then(
      (s) => {
        setSuites(s)
        setSuite((cur) => cur || s[0]?.name || "")
      },
      () => {},
    )
  }, [api, id])
  const loadRuns = useCallback(() => {
    api.get<RunInfo[]>(`agents/${id}/runs`).then(setRuns, () => {})
    api.get<Proposal[]>(`agents/${id}/proposals`).then(setProposals, () => {})
  }, [api, id])
  useEffect(loadSuites, [loadSuites])
  useEffect(loadRuns, [loadRuns])
  useEffect(() => {
    const s = suites.find((x) => x.name === suite)
    setSource(s?.source ?? "")
    setEstimate(null)
    if (suite) api.get<Estimate>(`agents/${id}/suites/${suite}/estimate`).then(setEstimate, () => {})
  }, [suite, suites, api, id])
  const openRun = useCallback(
    async (rid: string) => {
      const r = await api.get<Run>(`agents/${id}/runs/${rid}`)
      setRun(r)
      if (r.previous && r.status !== "running")
        setCmp(await api.get<Comparison>(`agents/${id}/compare?a=${r.previous}&b=${r.id}`))
      else setCmp(null)
    },
    [api, id],
  )
  useEvent<{ agent?: string; run?: string }>("workbench.run", (ev) => {
    if (ev.data?.agent !== id) return
    loadRuns()
    if (ev.data.run && (run?.id === ev.data.run || !run)) void openRun(ev.data.run)
  })
  useEvent<{ agent?: string }>("workbench.proposal", (ev) => ev.data?.agent === id && loadRuns())

  const saveSuite = async () => {
    try {
      const s = await api.put<{ name: string }>(`agents/${id}/suites`, { source })
      setNotice(`Suite ${s.name} saved.`)
      setSuite(s.name)
      loadSuites()
    } catch (e) {
      setNotice(errText(e))
    }
  }
  const start = async (mode: "offline" | "live") => {
    if (dirty) {
      setNotice("Save the agent first: a run uses the saved files.")
      return
    }
    if (mode === "offline") {
      try {
        const r = await api.post<Run>(`agents/${id}/suites/${suite}/run`, { mode })
        setRun(r)
        setCmp(null)
        loadRuns()
      } catch (e) {
        setNotice(errText(e))
      }
      return
    }
    if (!estimate) return
    const ok = await desk.confirm({
      title: `Run ${estimate.cases} cases against ${estimate.model}?`,
      message: `This calls a real model through ${estimate.gateway} and costs money. Estimated ${estimate.tokens.toLocaleString()} tokens, at most about $${estimate.worst_usd.toFixed(2)} at a conservative price. Caps: $${estimate.caps.max_usd} (estimated at a conservative price), ${estimate.caps.turn_tokens.toLocaleString()} tokens a case, ${estimate.caps.run_tokens.toLocaleString()} tokens for the run.`,
      confirmLabel: "Run",
    })
    if (!ok) return
    try {
      setBusy("run")
      const r = await api.post<Run>(`agents/${id}/suites/${suite}/run`, { mode: "live", confirm: true })
      setRun(r)
      setCmp(null)
      loadRuns()
    } catch (e) {
      setNotice(errText(e))
    } finally {
      setBusy("")
    }
  }
  const rerun = async (r: Run) => {
    if (dirty) {
      setNotice("Save the agent first: a run uses the saved files.")
      return
    }
    if (r.mode !== "live") {
      try {
        const next = await api.post<Run>(`agents/${id}/runs/${r.id}/rerun`, { mode: "offline" })
        setRun(next)
        loadRuns()
      } catch (e) {
        setNotice(errText(e))
      }
      return
    }
    let est: Estimate
    try {
      est = await api.get<Estimate>(`agents/${id}/suites/${r.suite}/estimate`)
    } catch (e) {
      setNotice(errText(e))
      return
    }
    const ok = await desk.confirm({
      title: `Run ${r.suite} again (${est.cases} cases, ${est.model})?`,
      message: `This calls a real model and costs money. Estimated ${est.tokens.toLocaleString()} tokens, at most about $${est.worst_usd.toFixed(2)} at a conservative price. Caps: $${est.caps.max_usd}, ${est.caps.turn_tokens.toLocaleString()} tokens a case, ${est.caps.run_tokens.toLocaleString()} tokens for the run. The result is compared with this run.`,
      confirmLabel: "Run again",
    })
    if (!ok) return
    try {
      const next = await api.post<Run>(`agents/${id}/runs/${r.id}/rerun`, { mode: "live", confirm: true })
      setRun(next)
      loadRuns()
    } catch (e) {
      setNotice(errText(e))
    }
  }
  const propose = async (r: Run) => {
    let est: { model: string; input_tokens: number; max_output: number; worst_usd: number; cap_usd: number }
    try {
      est = await api.get(`agents/${id}/runs/${r.id}/propose-estimate`)
    } catch (e) {
      setNotice(errText(e))
      return
    }
    const ok = await desk.confirm({
      title: "Ask a model for improvements?",
      message: `The ${r.failed} failed cases, the prompt, skills, variants, context and reflexes are sent to ${est.model}: about ${est.input_tokens.toLocaleString()} tokens in, at most ${est.max_output.toLocaleString()} out, at most about $${est.worst_usd.toFixed(2)} at a conservative price (cap $${est.cap_usd}). It costs money. Nothing changes until you accept an edit.`,
      confirmLabel: "Propose",
    })
    if (!ok) return
    try {
      setBusy("propose")
      await api.post(`agents/${id}/runs/${r.id}/propose`, { confirm: true })
      loadRuns()
    } catch (e) {
      setNotice(errText(e))
    } finally {
      setBusy("")
    }
  }
  const decide = async (p: Proposal, n: number, d: "accept" | "reject") => {
    try {
      await api.post(`agents/${id}/proposals/${p.id}/${n}/${d}`, {})
      loadRuns()
    } catch (e) {
      setNotice(errText(e))
      loadRuns()
    }
  }
  const compareWith = async (other: string) => {
    if (!run || !other) return
    setCmp(await api.get<Comparison>(`agents/${id}/compare?a=${other}&b=${run.id}`))
  }

  return (
    <div className="flex min-h-0 flex-col">
      {notice && (
        <div className="flex items-center gap-2 bg-warning/10 px-3 py-1.5 text-xs">
          <span className="flex-1">{notice}</span>
          <Button variant="ghost" size="xs" onClick={() => setNotice("")}>
            Dismiss
          </Button>
        </div>
      )}
      <Section
        title="Suites (workbench/evals/*.yaml)"
        aside={
          <div className="flex gap-1">
            {suites.map((s) => (
              <Button
                key={s.name}
                size="xs"
                variant={s.name === suite ? "secondary" : "ghost"}
                onClick={() => setSuite(s.name)}
              >
                {s.name} ({s.cases})
              </Button>
            ))}
            <Button
              size="xs"
              variant="outline"
              onClick={() => {
                setSuite("")
                setSource(SUITE_EXAMPLE)
              }}
            >
              <Plus /> Suite
            </Button>
          </div>
        }
      >
        {suites.find((s) => s.name === suite)?.error && (
          <p className="text-xs text-destructive">{suites.find((s) => s.name === suite)?.error}</p>
        )}
        <CodeEditor value={source} onChange={setSource} rows={12} />
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => void saveSuite()}>
            <Save /> Save suite
          </Button>
          {estimate && (
            <span className={hint}>
              {estimate.cases} cases · {estimate.model} · ~{estimate.tokens.toLocaleString()} tokens est. (prompt ~
              {estimate.prompt_tokens.toLocaleString()}) · caps ${estimate.caps.max_usd} /{" "}
              {estimate.caps.turn_tokens.toLocaleString()} a case / {estimate.caps.run_tokens.toLocaleString()} a run
              {!estimate.ready && <span className="text-destructive"> · not ready: {estimate.not_ready}</span>}
            </span>
          )}
          <Button size="sm" variant="outline" disabled={!suite || busy !== ""} onClick={() => void start("offline")}>
            <Play /> Run offline
          </Button>
          <Button size="sm" disabled={!estimate?.ready || busy !== ""} onClick={() => void start("live")}>
            <Play /> Run live…
          </Button>
        </div>
        <p className={hint}>
          Graders: contains, not_contains, regex, answer_line, json_path, tool_called (with args), no_tool_called,
          max_steps, max_tokens, file, rubric (model-graded; live only). Offline runs play each case's script (a harness
          check, no model, nothing spent); live runs call the app's gateway after you confirm. Cases run through
          Nanite's eval harness with the agent's saved files (digest {short(digest)}); fixtures are the workspace; no
          MCP server is started. Edit a case’s stubs above to define tool names, descriptions, schemas and canned
          results. Use when to match arguments; unmatched calls consume results in order, repeating the last. Names
          beginning with mcp__ and built-in tool names are reserved.
        </p>
      </Section>
      <div className="flex min-h-0 flex-1">
        <ul className="flex w-72 shrink-0 flex-col gap-0.5 border-r border-border p-1">
          {runs.map((r) => (
            <li key={r.id}>
              <Item
                size="xs"
                variant={run?.id === r.id ? "muted" : "default"}
                render={
                  <a
                    href={`#run-${r.id}`}
                    onClick={(e) => {
                      e.preventDefault()
                      void openRun(r.id)
                    }}
                  />
                }
              >
                <ItemContent className="min-w-0">
                  <ItemTitle className="w-full truncate">
                    {r.suite} · v{r.version} {r.mode === "live" ? "· live" : ""}{" "}
                    <Badge variant={r.failed ? "destructive" : "secondary"}>
                      {r.passed}/{r.passed + r.failed}
                    </Badge>
                    {r.status !== "done" && <Badge variant="outline">{r.status}</Badge>}
                  </ItemTitle>
                  <ItemDescription>
                    {when(r.started)} · {r.tokens.toLocaleString()} tok · ${r.spent_usd.toFixed(3)}
                  </ItemDescription>
                </ItemContent>
              </Item>
            </li>
          ))}
          {runs.length === 0 && <li className={`${hint} p-2`}>No runs yet.</li>}
        </ul>
        <div className="min-w-0 flex-1 overflow-auto">
          {run ? (
            <RunView
              run={run}
              runs={runs}
              cmp={cmp}
              busy={busy}
              proposals={proposals.filter((p) => p.run === run.id)}
              onRerun={() => void rerun(run)}
              onPropose={() => void propose(run)}
              onCompare={(o) => void compareWith(o)}
              onDecide={(p, n, d) => void decide(p, n, d)}
              onRefresh={() => void openRun(run.id)}
            />
          ) : (
            <p className={`${hint} p-3`}>Pick a run.</p>
          )}
        </div>
      </div>
    </div>
  )
}

function RunView({
  run,
  runs,
  cmp,
  busy,
  proposals,
  onRerun,
  onPropose,
  onCompare,
  onDecide,
  onRefresh,
}: {
  run: Run
  runs: RunInfo[]
  cmp: Comparison | null
  busy: string
  proposals: Proposal[]
  onRerun: () => void
  onPropose: () => void
  onCompare: (other: string) => void
  onDecide: (p: Proposal, n: number, d: "accept" | "reject") => void
  onRefresh: () => void
}) {
  const others = runs.filter((r) => r.id !== run.id && r.suite === run.suite)
  return (
    <>
      <Section
        title={`Run ${run.id}`}
        aside={
          <div className="flex gap-1">
            <Button size="xs" variant="ghost" onClick={onRefresh} aria-label="Refresh">
              <RefreshCw />
            </Button>
            <Button size="xs" variant="outline" disabled={run.status === "running"} onClick={onRerun}>
              <Play /> Re-run
            </Button>
            <Button
              size="xs"
              disabled={run.status === "running" || run.failed === 0 || busy !== ""}
              onClick={onPropose}
            >
              <Sparkles /> Propose improvements
            </Button>
          </div>
        }
      >
        <p className={hint}>
          {run.mode} · {run.status}
          {run.stopped && `: ${run.stopped}`} · {run.model} · digest {short(run.digest)} (v{run.version}) · {run.passed}{" "}
          passed, {run.failed} failed · {run.tokens.toLocaleString()} tokens · ${run.spent_usd.toFixed(4)} est. · caps $
          {run.caps.max_usd}
        </p>
        {others.length > 0 && (
          <div className="flex items-center gap-2">
            <span className={hint}>Compare with</span>
            <Select
              value=""
              onValueChange={(v) => v && onCompare(String(v))}
              items={others.map((o) => ({ value: o.id, label: `${o.id} (v${o.version})` }))}
            >
              <SelectTrigger size="sm" aria-label="Compare with">
                <SelectValue placeholder="another run" />
              </SelectTrigger>
              <SelectContent>
                {others.map((o) => (
                  <SelectItem key={o.id} value={o.id}>
                    {o.id} · v{o.version} · {o.passed}/{o.passed + o.failed}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
      </Section>
      {cmp && (
        <Section title={`Compared with ${cmp.a.id} (v${cmp.a.version})`}>
          <p className="text-xs">
            {cmp.fixed} fixed · {cmp.broken} broken · {cmp.a.passed}/{cmp.a.passed + cmp.a.failed} → {cmp.b.passed}/
            {cmp.b.passed + cmp.b.failed}
          </p>
          <ul className="flex flex-col gap-0.5 text-xs">
            {cmp.cases.map((c) => (
              <li key={c.id} className="flex gap-2 font-mono">
                <span className="w-48 truncate">{c.id}</span>
                <span>{c.a === undefined ? "–" : c.a ? "pass" : "fail"}</span>→
                <span>{c.b === undefined ? "–" : c.b ? "pass" : "fail"}</span>
                <Badge variant={c.change === "fixed" ? "secondary" : c.change === "broken" ? "destructive" : "outline"}>
                  {c.change}
                </Badge>
                <span className="text-muted-foreground">
                  tokens {c.a_tokens}→{c.b_tokens} · steps {c.a_steps}→{c.b_steps}
                </span>
              </li>
            ))}
          </ul>
        </Section>
      )}
      {proposals.map((p) => (
        <Section
          key={p.id}
          title={`Proposal ${p.id}`}
          aside={
            <span className={hint}>
              {p.model} · {p.tokens} tokens
            </span>
          }
        >
          {p.error && <p className="text-xs text-destructive">{p.error}</p>}
          {p.items.map((e) => (
            <div key={e.n} className="flex flex-col gap-1 rounded-md border border-border p-2">
              <div className="flex items-center gap-2">
                <span className="flex-1 font-mono text-xs">
                  #{e.n} {e.file}
                </span>
                <Badge
                  variant={e.status === "accepted" ? "secondary" : e.status === "pending" ? "outline" : "destructive"}
                >
                  {e.status}
                  {e.version ? ` → v${e.version}` : ""}
                </Badge>
                {e.status === "pending" && (
                  <>
                    <Button size="xs" onClick={() => onDecide(p, e.n, "accept")}>
                      Accept
                    </Button>
                    <Button size="xs" variant="ghost" onClick={() => onDecide(p, e.n, "reject")}>
                      Reject
                    </Button>
                  </>
                )}
              </div>
              <p className="text-xs">{e.rationale}</p>
              {e.problem && <p className="text-xs text-destructive">{e.problem}</p>}
              <DiffView lines={unifiedLines(e.diff)} />
            </div>
          ))}
        </Section>
      ))}
      {run.cases.map((c) => (
        <Section
          key={c.id}
          title={c.id}
          aside={<Badge variant={c.pass ? "secondary" : "destructive"}>{c.pass ? "pass" : "fail"}</Badge>}
        >
          {c.why && <p className="text-xs text-destructive">{c.why}</p>}
          <ul className="flex flex-col gap-0.5 text-xs">
            {c.graders.map((g, i) => (
              <li
                // biome-ignore lint/suspicious/noArrayIndexKey: graders are listed in order
                key={i}
              >
                {g.skipped ? "–" : g.pass ? "✓" : "✗"} <span className="font-mono">{g.kind}</span> {g.detail}
              </li>
            ))}
          </ul>
          <p className={hint}>
            {c.steps} steps · {c.tokens.toLocaleString()} tokens · {c.wall_ms} ms
            {c.stub_calls &&
              ` · stub calls: ${Object.entries(c.stub_calls)
                .map(([name, count]) => `${name} × ${count}`)
                .join(", ")}`}
          </p>
          <Collapsible>
            <CollapsibleTrigger render={<Button size="xs" variant="ghost" className="self-start" />}>
              Answer and transcript
            </CollapsibleTrigger>
            <CollapsibleContent>
              <pre className="mb-2 rounded-md bg-muted/40 p-2 text-xs whitespace-pre-wrap">
                {c.answer || "(no answer)"}
              </pre>
              {c.transcript.map((x, i) => (
                <div
                  // biome-ignore lint/suspicious/noArrayIndexKey: exchanges are in order
                  key={i}
                  className="mb-2 border-l-2 border-border pl-2 text-xs"
                >
                  <div className="text-muted-foreground">
                    {x.subtask ? "subtask " : ""}sent ({x.tokens} tokens):
                  </div>
                  <pre className="whitespace-pre-wrap">{x.sent}</pre>
                  {x.reply && <pre className="whitespace-pre-wrap text-foreground">→ {x.reply}</pre>}
                  {(x.calls ?? []).map((call, k) => (
                    <pre
                      // biome-ignore lint/suspicious/noArrayIndexKey: calls are in order
                      key={k}
                      className="whitespace-pre-wrap font-mono"
                    >
                      → {call.name} {JSON.stringify(call.input)}
                    </pre>
                  ))}
                </div>
              ))}
            </CollapsibleContent>
          </Collapsible>
        </Section>
      ))}
    </>
  )
}

// ---- Assembled prompt ----

function PreviewTab({ api, id, version }: { api: ModuleAPI; id: string; version: number }) {
  const [p, setP] = useState<Preview | null>(null)
  const [err, setErr] = useState("")
  // biome-ignore lint/correctness/useExhaustiveDependencies: assemble again after each save (a new version)
  useEffect(() => {
    api.get<Preview>(`agents/${id}/preview`).then(setP, (e) => setErr(errText(e)))
  }, [api, id, version])
  if (err) return <p className="p-3 text-xs text-destructive">{err}</p>
  if (!p) return <p className={`${hint} p-3`}>Assembling…</p>
  return (
    <>
      <Section title={`The first request, as Nanite assembles it (saved files, ~${p.tokens.toLocaleString()} tokens)`}>
        <p className={hint}>
          No model is called: the agent is opened on a copy with a stand-in provider and its first request is shown.
          Tools here are the skill tools only; a host adds its own, and eval runs add the file tools.
        </p>
        <p className="text-xs">Tools: {p.tools.join(", ") || "none"}</p>
      </Section>
      {p.slots.map((s) => (
        <Section
          key={s.name}
          title={`${s.name} · ~${s.tokens} tokens`}
          aside={
            s.read_only ? (
              <Badge variant="outline">engine-owned, read-only</Badge>
            ) : (
              <Badge variant="secondary">from the agent</Badge>
            )
          }
        >
          <pre className="max-h-80 overflow-auto rounded-md bg-muted/40 p-2 text-xs whitespace-pre-wrap">
            {s.content}
          </pre>
        </Section>
      ))}
    </>
  )
}

export default { views: { "workbench/main": WorkbenchView } } satisfies ModuleWeb
