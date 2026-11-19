/**
 * Drafts: documents the owner and the agent work on together. Read shows the draft as it reads
 * (Markdown rendered; code coloured as its language). Review shows the text by line:
 * select a passage and mark it. Edit changes the text; every save is a revision. Compare shows
 * any revision against the latest. The marks on the draft are listed beside the text (under
 * it when the window is narrow).
 */
import {
  ApiError,
  addMark,
  type Mark,
  type ModuleWeb,
  replyMark,
  resolveMark,
  useAnchor,
  useEvent,
  useMarks,
  type ViewProps,
} from "@hollis-labs/surface-page/sdk"
import {
  BookOpen,
  Check,
  FilePlus2,
  FileText,
  GitCompare,
  List,
  type LucideIcon,
  MessageSquarePlus,
  Pencil,
  Save,
  ScanText,
} from "lucide-react"
import { type ReactElement, type ReactNode, useCallback, useEffect, useId, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Field, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Item, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item"
import { Kbd } from "@/components/ui/kbd"
import { Markdown } from "@/components/ui/markdown"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { TextEditor } from "@/components/ui/text-editor"
import { Textarea } from "@/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"

interface Draft {
  id: string
  title: string
  lang: string
  body?: string
  rev: number
  updated: string
  lines?: number
}
interface Revision {
  rev: number
  author: string
  summary?: string
  created: string
}
interface DiffLine {
  op: " " | "+" | "-"
  text: string
}
type Mode = "read" | "review" | "edit" | "compare"
const MODES: { mode: Mode; label: string; icon: LucideIcon }[] = [
  { mode: "read", label: "Read", icon: BookOpen },
  { mode: "review", label: "Review", icon: ScanText },
  { mode: "edit", label: "Edit", icon: Pencil },
  { mode: "compare", label: "Compare", icon: GitCompare },
]

/** A control with its name (and shortcut) in a tooltip. */
function Tip({ label, children }: { label: ReactNode; children: ReactElement }) {
  return (
    <Tooltip>
      <TooltipTrigger render={children} />
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

const hint = "px-2.5 text-xs text-muted-foreground"

/** A draft as it reads: Markdown (and plain text) as is; anything else as a block of code in its language. */
function readable(body: string, lang: string): string {
  if (!lang || lang === "markdown" || lang === "md" || lang === "text") return body
  const longest = Math.max(0, ...(body.match(/`+/g) ?? []).map((r) => r.length))
  const fence = "`".repeat(Math.max(3, longest + 1))
  return `${fence}${lang}\n${body.replace(/\n$/, "")}\n${fence}`
}

function when(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })
}

function DraftsView({ win, desk, api }: ViewProps) {
  const id = typeof win.args.id === "string" ? win.args.id : ""
  return (
    <div className="@container flex h-full min-h-0 flex-col">
      {id ? (
        <DraftEditor key={id} id={id} win={win} desk={desk} api={api} />
      ) : (
        <DraftList desk={desk} api={api} win={win} />
      )}
    </div>
  )
}

/** No draft chosen: the list, and a way to start one. */
function DraftList({ win, desk, api }: ViewProps) {
  const [drafts, setDrafts] = useState<Draft[]>([])
  const [q, setQ] = useState("")
  const load = useCallback(() => {
    api.get<Draft[]>(`drafts?q=${encodeURIComponent(q)}`).then(setDrafts, () => {})
  }, [api, q])
  useEffect(load, [load])
  useEvent("drafts.changed", load)

  const show = (d: Draft) => {
    void desk.setArgs(win.id, { id: d.id })
    void desk.setTitle(win.id, d.title)
  }
  const create = async () => show(await api.post<Draft>("drafts", { title: "Untitled" }))
  return (
    <>
      <div className="flex gap-1.5 border-b border-border p-2">
        <Input
          className="min-w-0 flex-1"
          placeholder="Find a draft"
          aria-label="Find a draft"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <Button onClick={() => void create()}>
          <FilePlus2 /> New
        </Button>
      </div>
      {drafts.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FileText />
            </EmptyMedia>
            <EmptyTitle>{q ? "No draft matches" : "No drafts yet"}</EmptyTitle>
            <EmptyDescription>Start one with New, or ask the agent to write one.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <ul className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-auto p-1">
          {drafts.map((d) => (
            <li key={d.id}>
              <Item
                size="xs"
                render={
                  <a
                    href={`#draft-${d.id}`}
                    onClick={(e) => {
                      e.preventDefault()
                      show(d)
                    }}
                  />
                }
              >
                <ItemContent className="min-w-0">
                  <ItemTitle className="w-full truncate">{d.title}</ItemTitle>
                  <ItemDescription>
                    {d.lang} · r{d.rev} · {d.lines ?? 0} lines · {when(d.updated)}
                  </ItemDescription>
                </ItemContent>
              </Item>
            </li>
          ))}
        </ul>
      )}
    </>
  )
}

function DraftEditor({ id, win, desk, api }: ViewProps & { id: string }) {
  const [draft, setDraft] = useState<Draft | null>(null)
  const [mode, setMode] = useState<Mode>((win.state?.mode as Mode) || "read")
  const [text, setText] = useState("")
  const [title, setTitle] = useState("")
  const [dirty, setDirty] = useState(false)
  const [notice, setNotice] = useState("")
  const [revs, setRevs] = useState<Revision[]>([])
  const [from, setFrom] = useState(0)
  const [diff, setDiff] = useState<DiffLine[]>([])
  const [picked, setPicked] = useState<{ quote: string; start: number; end: number } | null>(null)
  const [composing, setComposing] = useState("")
  const ref = `drafts:${id}`
  const marks = useMarks(ref)
  const body = useRef<HTMLDivElement>(null)
  const modesAnchor = useAnchor(win, "modes", "Read, Review, Edit and Compare")
  const textAnchor = useAnchor(win, "text", "The draft's text")
  const marksAnchor = useAnchor(win, "marks", "The marks on the draft")

  const load = useCallback(async () => {
    try {
      const d = await api.get<Draft>(`drafts/${id}`)
      setDraft(d)
      setText(d.body ?? "")
      setTitle(d.title)
      setDirty(false)
      desk.setDirty(win.id, false)
      setNotice("")
    } catch (e) {
      setNotice(e instanceof Error ? e.message : String(e))
    }
  }, [api, id, desk, win.id])
  useEffect(() => {
    void load()
  }, [load])
  // A window opened by a layout or the agent may carry another title: show the draft's.
  const savedTitle = draft?.title
  useEffect(() => {
    if (savedTitle && savedTitle !== win.title) void desk.setTitle(win.id, savedTitle)
  }, [savedTitle, win.title, win.id, desk])

  // Another hand (the agent, another window) saved it.
  useEvent<{ id: string; rev: number; author: string }>("drafts.changed", (ev) => {
    if (ev.data?.id !== id) return
    if (dirty) setNotice(`Revision ${ev.data.rev} was saved by ${ev.data.author} while you were editing.`)
    else void load()
  })

  useEffect(() => {
    if (mode !== "compare" || !draft) return
    api.get<Revision[]>(`drafts/${id}/revisions`).then(setRevs, () => {})
  }, [mode, draft, api, id])
  useEffect(() => {
    if (mode !== "compare" || !draft) return
    const f = from || Math.max(1, draft.rev - 1)
    api.get<{ lines: DiffLine[] }>(`drafts/${id}/diff?from=${f}&to=${draft.rev}`).then(
      (r) => setDiff(r.lines ?? []),
      () => {},
    )
  }, [mode, from, draft, api, id])

  // desk.report replaces the window's state: every report says the whole of it.
  const report = (m: Mode, sel?: { quote: string; start: number; end: number } | null) =>
    desk.report(
      win.id,
      sel ? { mode: m, selection: sel.quote.slice(0, 2000), line_start: sel.start, line_end: sel.end } : { mode: m },
    )

  const switchMode = (m: Mode) => {
    setMode(m)
    setPicked(null)
    report(m)
  }

  const markDirty = () => {
    if (!dirty) {
      setDirty(true)
      desk.setDirty(win.id, true)
    }
  }

  const save = async () => {
    if (!draft) return
    try {
      const d = await api.put<Draft>(`drafts/${id}`, {
        body: text,
        title,
        expect_rev: draft.rev,
        summary: "edited by the owner",
      })
      setDraft(d)
      setDirty(false)
      desk.setDirty(win.id, false)
      setNotice("")
      if (d.title !== win.title) void desk.setTitle(win.id, d.title)
    } catch (e) {
      if (e instanceof ApiError && e.status === 409)
        setNotice("The draft changed since you opened it. Copy your text, then reload.")
      else setNotice(e instanceof Error ? e.message : String(e))
    }
  }

  // The selection in Review: which lines, and the text. Reported to the desk for the agent.
  const onSelect = () => {
    const sel = window.getSelection()
    const root = body.current
    if (!sel || !root || sel.isCollapsed || !root.contains(sel.anchorNode)) {
      if (picked) {
        setPicked(null)
        report(mode)
      }
      return
    }
    const lineOf = (n: Node | null) => {
      let el = n instanceof Element ? n : n?.parentElement
      while (el && !el.hasAttribute("data-line")) el = el.parentElement
      return el ? Number(el.getAttribute("data-line")) : 0
    }
    const a = lineOf(sel.anchorNode)
    const b = lineOf(sel.focusNode)
    const quote = sel.toString().trim()
    if (!quote) return
    const p = { quote, start: Math.min(a, b), end: Math.max(a, b) }
    setPicked(p)
    report(mode, p)
  }

  const mark = async () => {
    if (!picked || !composing.trim()) return
    await addMark({
      ref,
      body: composing.trim(),
      quote: picked.quote,
      anchor: { lines: { start: picked.start, end: picked.end } },
    })
    setComposing("")
    setPicked(null)
    report(mode)
  }

  if (!draft) return <p className={`${hint} py-2`}>{notice || "Loading…"}</p>
  const lines = (draft.body ?? "").replace(/\n$/, "").split("\n")
  const marked = new Set<number>()
  for (const m of marks) {
    const l = (m.anchor as { lines?: { start: number; end?: number } } | undefined)?.lines
    if (m.status === "open" && l) for (let i = l.start; i <= (l.end ?? l.start); i++) marked.add(i)
  }
  const fromRev = from || Math.max(1, draft.rev - 1)
  const older = revs
    .filter((r) => r.rev < draft.rev)
    .map((r) => ({ value: String(r.rev), label: `r${r.rev} · ${r.author} · ${r.summary || when(r.created)}` }))

  return (
    <>
      <div className="flex min-h-10 items-center gap-1 border-b border-border px-2 py-1">
        <Tip label="All drafts">
          <Button variant="ghost" size="icon-sm" aria-label="All drafts" onClick={() => void desk.setArgs(win.id, {})}>
            <List />
          </Button>
        </Tip>
        {mode === "edit" ? (
          <Input
            className="min-w-0 flex-1"
            value={title}
            aria-label="Title"
            onChange={(e) => {
              setTitle(e.target.value)
              markDirty()
            }}
          />
        ) : (
          <span className="min-w-0 flex-1 truncate text-sm font-semibold">{draft.title}</span>
        )}
        <span className="text-xs whitespace-nowrap text-muted-foreground @max-md:hidden">
          {draft.lang} · r{draft.rev}
        </span>
        <ToggleGroup
          ref={modesAnchor}
          size="sm"
          spacing={0}
          value={[mode]}
          onValueChange={(v: string[]) => v[0] && switchMode(v[0] as Mode)}
        >
          {MODES.map(({ mode: m, label, icon: ModeIcon }) => (
            <Tip key={m} label={label}>
              <ToggleGroupItem value={m} aria-label={label}>
                <ModeIcon />
                <span className="@max-md:hidden">{label}</span>
              </ToggleGroupItem>
            </Tip>
          ))}
        </ToggleGroup>
        {mode === "edit" && (
          <Tip
            label={
              <>
                Save <Kbd>⌘S</Kbd>
              </>
            }
          >
            <Button size="sm" aria-label="Save" disabled={!dirty} onClick={() => void save()}>
              <Save />
              <span className="@max-md:hidden">Save</span>
            </Button>
          </Tip>
        )}
      </div>
      {notice && (
        <div className="flex items-center gap-2 bg-warning/10 px-3 py-1.5 text-xs">
          <span className="flex-1">{notice}</span>
          <Button variant="outline" size="xs" onClick={() => void load()}>
            Reload
          </Button>
        </div>
      )}
      <div className="flex min-h-0 flex-1 @max-2xl:flex-col">
        <div ref={textAnchor} className="min-h-0 min-w-0 flex-1 overflow-auto">
          {mode === "read" && (
            <article className="mx-auto max-w-3xl px-5 pt-3 pb-8">
              <Markdown size="document">{readable(draft.body ?? "", draft.lang)}</Markdown>
            </article>
          )}
          {mode === "review" && (
            <div ref={body} className="py-2 font-mono text-xs leading-5" onMouseUp={onSelect} onKeyUp={onSelect}>
              {lines.map((l, i) => (
                <div
                  // biome-ignore lint/suspicious/noArrayIndexKey: a line is its position
                  key={i}
                  data-line={i + 1}
                  className={marked.has(i + 1) ? "flex bg-warning/15" : "flex"}
                >
                  <span className="w-11 shrink-0 pr-3 text-right text-muted-foreground/60 select-none @max-md:w-8 @max-md:pr-2">
                    {i + 1}
                  </span>
                  <span className="min-w-0 flex-1 pr-3 break-words whitespace-pre-wrap">{l || " "}</span>
                </div>
              ))}
            </div>
          )}
          {mode === "edit" && (
            <TextEditor
              value={text}
              markdown={draft.lang === "markdown" || draft.lang === "text"}
              aria-label="Text"
              spellCheck={draft.lang === "markdown" || draft.lang === "text"}
              onChange={(e) => {
                setText(e.target.value)
                markDirty()
              }}
              onKeyDown={(e) => {
                if ((e.metaKey || e.ctrlKey) && e.key === "s") {
                  e.preventDefault()
                  void save()
                }
              }}
            />
          )}
          {mode === "compare" && (
            <>
              <div className="flex flex-wrap items-center gap-2 px-3 py-2 text-xs text-muted-foreground">
                <span>Revision</span>
                <Select value={String(fromRev)} onValueChange={(v) => setFrom(Number(v))} items={older}>
                  <SelectTrigger size="sm" aria-label="Revision to compare">
                    <SelectValue placeholder={`r${fromRev}`} />
                  </SelectTrigger>
                  <SelectContent>
                    {older.map((o) => (
                      <SelectItem key={o.value} value={o.value}>
                        {o.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <span>against the latest (r{draft.rev})</span>
              </div>
              <div className="font-mono text-xs leading-5">
                {draft.rev === 1 && <p className={hint}>There is only one revision.</p>}
                {diff.map((d, i) => (
                  <div
                    // biome-ignore lint/suspicious/noArrayIndexKey: a diff line is its position
                    key={i}
                    className={`px-3 break-words whitespace-pre-wrap ${d.op === "+" ? "bg-success/15" : d.op === "-" ? "bg-destructive/10 line-through decoration-destructive/50" : ""}`}
                  >
                    <span className="text-muted-foreground/60 select-none">{d.op} </span>
                    {d.text || " "}
                  </div>
                ))}
              </div>
            </>
          )}
        </div>
        <MarksPanel
          anchor={marksAnchor}
          marks={marks}
          picked={picked}
          composing={composing}
          setComposing={setComposing}
          onMark={() => void mark()}
        />
      </div>
    </>
  )
}

function MarksPanel({
  anchor,
  marks,
  picked,
  composing,
  setComposing,
  onMark,
}: {
  anchor?: (el: HTMLElement | null) => void
  marks: Mark[]
  picked: { quote: string; start: number; end: number } | null
  composing: string
  setComposing: (s: string) => void
  onMark: () => void
}) {
  const [showDone, setShowDone] = useState(false)
  const id = useId()
  const list = marks.filter((m) => showDone || m.status === "open")
  const open = marks.filter((m) => m.status === "open").length
  return (
    <aside
      ref={anchor}
      className="flex min-h-0 w-66 shrink-0 flex-col border-l border-border bg-card @max-2xl:max-h-[42%] @max-2xl:w-full @max-2xl:border-t @max-2xl:border-l-0"
    >
      <div className="flex items-center justify-between gap-2 px-2.5 py-2">
        <span className="text-xs font-semibold">Marks ({open} open)</span>
        <Field orientation="horizontal" className="w-auto">
          <Checkbox id={`${id}-done`} checked={showDone} onCheckedChange={(on) => setShowDone(on === true)} />
          <FieldLabel htmlFor={`${id}-done`}>resolved</FieldLabel>
        </Field>
      </div>
      {picked && (
        <div className="mx-2 mb-2 flex flex-col gap-1.5 rounded-md border border-border bg-background p-2 text-xs">
          <div className="line-clamp-2 text-muted-foreground italic">
            Lines {picked.start}
            {picked.end !== picked.start ? `–${picked.end}` : ""}: “{picked.quote}”
          </div>
          <Textarea
            placeholder="A comment, a question, a change to make"
            aria-label="Mark"
            rows={3}
            value={composing}
            onChange={(e) => setComposing(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") onMark()
            }}
          />
          <Tip
            label={
              <>
                Mark <Kbd>⌘↵</Kbd>
              </>
            }
          >
            <Button size="sm" className="self-start" disabled={!composing.trim()} onClick={onMark}>
              <MessageSquarePlus /> Mark
            </Button>
          </Tip>
        </div>
      )}
      {!picked && list.length === 0 && (
        <p className={hint}>Select a passage in Review to mark it. The agent sees your marks and answers them.</p>
      )}
      <ul className="flex min-h-0 flex-1 flex-col gap-2 overflow-auto px-2 pb-2">
        {list.map((m) => (
          <MarkCard key={m.id} mark={m} />
        ))}
      </ul>
    </aside>
  )
}

function MarkCard({ mark }: { mark: Mark }) {
  const [reply, setReply] = useState("")
  const send = async () => {
    if (!reply.trim()) return
    await replyMark(mark.id, reply.trim())
    setReply("")
  }
  return (
    <li
      className={`rounded-md border border-border bg-background p-2 text-xs ${mark.status === "resolved" ? "opacity-60" : ""}`}
    >
      {mark.quote && <div className="mb-1 line-clamp-2 text-muted-foreground italic">“{mark.quote}”</div>}
      <div>
        <span className="font-semibold">{mark.author}</span>
        <Markdown className="text-xs">{mark.body}</Markdown>
      </div>
      {mark.replies?.map((r) => (
        <div key={`${r.created}-${r.author}-${r.body.slice(0, 16)}`} className="mt-1 border-l-2 border-border pl-2">
          <span className="font-semibold">{r.author}</span>
          <Markdown className="text-xs">{r.body}</Markdown>
        </div>
      ))}
      {mark.status === "open" && (
        <div className="mt-1.5 flex gap-1">
          <Input
            className="min-w-0 flex-1"
            placeholder="Reply"
            aria-label="Reply"
            value={reply}
            onChange={(e) => setReply(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void send()
            }}
          />
          <Tip label="Resolve">
            <Button variant="ghost" size="icon" aria-label="Resolve" onClick={() => void resolveMark(mark.id)}>
              <Check />
            </Button>
          </Tip>
        </div>
      )}
    </li>
  )
}

const web: ModuleWeb = { views: { "drafts/main": DraftsView } }
export default web
