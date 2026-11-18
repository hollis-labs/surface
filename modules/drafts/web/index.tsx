/**
 * Drafts: documents the owner and the agent work on together. Review shows the text by line:
 * select a passage and mark it. Edit changes the text; every save is a revision. Compare shows
 * any revision against the latest. The marks on the draft are listed on the right.
 */
import {
  addMark,
  ApiError,
  Icon,
  type Mark,
  type ModuleWeb,
  replyMark,
  resolveMark,
  useEvent,
  useMarks,
  type ViewProps,
} from "@surface/sdk"
import { useCallback, useEffect, useRef, useState } from "react"

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
type Mode = "review" | "edit" | "compare"

const btn =
  "inline-flex items-center gap-1 rounded px-2 py-1 text-xs hover:bg-black/5 dark:hover:bg-white/10 disabled:opacity-40"
const btnOn = "bg-black/10 dark:bg-white/15"

function when(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })
}

function DraftsView({ win, desk, api }: ViewProps) {
  const id = typeof win.args.id === "string" ? win.args.id : ""
  return id ? <DraftEditor key={id} id={id} win={win} desk={desk} api={api} /> : <DraftList desk={desk} api={api} win={win} />
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
  const create = async () => {
    const d = await api.post<Draft>("drafts", { title: "Untitled" })
    show(d)
  }
  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 border-b border-black/10 p-2 dark:border-white/10">
        <input
          className="flex-1 rounded border border-black/15 bg-transparent px-2 py-1 text-sm dark:border-white/15"
          placeholder="Find a draft"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <button type="button" className={btn} onClick={() => void create()}>
          <Icon name="file-plus-2" size={14} /> New
        </button>
      </div>
      <ul className="flex-1 overflow-auto">
        {drafts.map((d) => (
          <li key={d.id}>
            <button
              type="button"
              className="flex w-full items-baseline gap-2 px-3 py-2 text-left hover:bg-black/5 dark:hover:bg-white/10"
              onClick={() => show(d)}
            >
              <span className="flex-1 truncate text-sm font-medium">{d.title}</span>
              <span className="text-xs opacity-60">
                {d.lang} · r{d.rev} · {d.lines ?? 0} lines · {when(d.updated)}
              </span>
            </button>
          </li>
        ))}
        {drafts.length === 0 && <li className="p-4 text-sm opacity-60">No drafts yet.</li>}
      </ul>
    </div>
  )
}

function DraftEditor({ id, win, desk, api }: ViewProps & { id: string }) {
  const [draft, setDraft] = useState<Draft | null>(null)
  const [mode, setMode] = useState<Mode>(((win.state?.mode as Mode) ?? "review") || "review")
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
    api.get<{ lines: DiffLine[] }>(`drafts/${id}/diff?from=${f}&to=${draft.rev}`).then((r) => setDiff(r.lines ?? []), () => {})
  }, [mode, from, draft, api, id])

  const switchMode = (m: Mode) => {
    setMode(m)
    desk.report(win.id, { mode: m })
  }

  const save = async () => {
    if (!draft) return
    try {
      const d = await api.put<Draft>(`drafts/${id}`, { body: text, title, expect_rev: draft.rev, summary: "edited by the owner" })
      setDraft(d)
      setDirty(false)
      desk.setDirty(win.id, false)
      setNotice("")
      if (d.title !== win.title) void desk.setTitle(win.id, d.title)
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) setNotice("The draft changed since you opened it. Copy your text, then reload.")
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
        desk.report(win.id, { mode })
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
    desk.report(win.id, { mode, selection: quote.slice(0, 2000), line_start: p.start, line_end: p.end })
  }

  const mark = async () => {
    if (!picked || !composing.trim()) return
    await addMark({ ref, body: composing.trim(), quote: picked.quote, anchor: { lines: { start: picked.start, end: picked.end } } })
    setComposing("")
    setPicked(null)
  }

  if (!draft) return <div className="p-4 text-sm opacity-70">{notice || "Loading…"}</div>
  const lines = (draft.body ?? "").split("\n")
  const marked = new Set<number>()
  for (const m of marks) {
    const l = (m.anchor as { lines?: { start: number; end?: number } } | undefined)?.lines
    if (m.status === "open" && l) for (let i = l.start; i <= (l.end ?? l.start); i++) marked.add(i)
  }

  return (
    <div className="flex h-full min-h-0 flex-col text-sm">
      <div className="flex items-center gap-1 border-b border-black/10 px-2 py-1 dark:border-white/10">
        <button type="button" className={btn} title="All drafts" onClick={() => void desk.setArgs(win.id, {})}>
          <Icon name="list" size={14} />
        </button>
        {mode === "edit" ? (
          <input
            className="mx-1 min-w-0 flex-1 rounded border border-black/15 bg-transparent px-2 py-0.5 dark:border-white/15"
            value={title}
            onChange={(e) => {
              setTitle(e.target.value)
              setDirty(true)
              desk.setDirty(win.id, true)
            }}
          />
        ) : (
          <span className="mx-1 min-w-0 flex-1 truncate font-medium">{draft.title}</span>
        )}
        <span className="text-xs opacity-60">
          {draft.lang} · r{draft.rev}
        </span>
        {(["review", "edit", "compare"] as const).map((m) => (
          <button key={m} type="button" className={`${btn} ${mode === m ? btnOn : ""}`} onClick={() => switchMode(m)}>
            <Icon name={{ review: "scan-text", edit: "pencil", compare: "git-compare" }[m]} size={14} />
            {m}
          </button>
        ))}
        {mode === "edit" && (
          <button type="button" className={btn} disabled={!dirty} onClick={() => void save()}>
            <Icon name="save" size={14} /> Save
          </button>
        )}
      </div>
      {notice && (
        <div className="flex items-center gap-2 bg-amber-500/15 px-3 py-1 text-xs">
          <span className="flex-1">{notice}</span>
          <button type="button" className={btn} onClick={() => void load()}>
            Reload
          </button>
        </div>
      )}
      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1 overflow-auto">
          {mode === "review" && (
            <div ref={body} className="py-2 font-mono text-[12.5px] leading-5" onMouseUp={onSelect} onKeyUp={onSelect}>
              {lines.map((l, i) => (
                <div key={i} data-line={i + 1} className={`flex ${marked.has(i + 1) ? "bg-yellow-400/20" : ""}`}>
                  <span className="w-12 shrink-0 select-none pr-3 text-right opacity-40">{i + 1}</span>
                  <span className="whitespace-pre-wrap break-words pr-3">{l || " "}</span>
                </div>
              ))}
            </div>
          )}
          {mode === "edit" && (
            <textarea
              className="h-full w-full resize-none bg-transparent p-3 font-mono text-[12.5px] leading-5 outline-none"
              value={text}
              spellCheck={draft.lang === "markdown" || draft.lang === "text"}
              onChange={(e) => {
                setText(e.target.value)
                if (!dirty) {
                  setDirty(true)
                  desk.setDirty(win.id, true)
                }
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
            <div className="py-2">
              <div className="flex items-center gap-2 px-3 pb-2 text-xs">
                <span className="opacity-70">Revision</span>
                <select
                  className="rounded border border-black/15 bg-transparent px-1 dark:border-white/15"
                  value={from || Math.max(1, draft.rev - 1)}
                  onChange={(e) => setFrom(Number(e.target.value))}
                >
                  {revs
                    .filter((r) => r.rev < draft.rev)
                    .map((r) => (
                      <option key={r.rev} value={r.rev}>
                        r{r.rev} · {r.author} · {r.summary || when(r.created)}
                      </option>
                    ))}
                </select>
                <span className="opacity-70">against the latest (r{draft.rev})</span>
              </div>
              <div className="font-mono text-[12.5px] leading-5">
                {draft.rev === 1 && <div className="px-3 opacity-60">There is only one revision.</div>}
                {diff.map((d, i) => (
                  <div
                    key={i}
                    className={`whitespace-pre-wrap break-words px-3 ${
                      d.op === "+" ? "bg-green-500/15" : d.op === "-" ? "bg-red-500/15 line-through decoration-red-500/40" : ""
                    }`}
                  >
                    <span className="select-none opacity-50">{d.op} </span>
                    {d.text || " "}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
        <MarksPanel
          marks={marks}
          picked={picked}
          composing={composing}
          setComposing={setComposing}
          onMark={() => void mark()}
        />
      </div>
    </div>
  )
}

function MarksPanel({
  marks,
  picked,
  composing,
  setComposing,
  onMark,
}: {
  marks: Mark[]
  picked: { quote: string; start: number; end: number } | null
  composing: string
  setComposing: (s: string) => void
  onMark: () => void
}) {
  const [showDone, setShowDone] = useState(false)
  const list = marks.filter((m) => showDone || m.status === "open")
  return (
    <aside className="flex w-72 shrink-0 flex-col border-l border-black/10 dark:border-white/10">
      <div className="flex items-center justify-between px-3 py-2 text-xs">
        <span className="font-medium">Marks ({marks.filter((m) => m.status === "open").length} open)</span>
        <label className="flex items-center gap-1 opacity-70">
          <input type="checkbox" checked={showDone} onChange={(e) => setShowDone(e.target.checked)} /> resolved
        </label>
      </div>
      {picked && (
        <div className="mx-2 mb-2 rounded border border-black/15 p-2 dark:border-white/15">
          <div className="mb-1 line-clamp-3 text-xs italic opacity-70">
            Lines {picked.start}
            {picked.end !== picked.start ? `–${picked.end}` : ""}: “{picked.quote}”
          </div>
          <textarea
            className="h-16 w-full resize-none rounded border border-black/15 bg-transparent p-1 text-xs dark:border-white/15"
            placeholder="A comment, a question, a change to make"
            value={composing}
            onChange={(e) => setComposing(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") onMark()
            }}
          />
          <button type="button" className={btn} disabled={!composing.trim()} onClick={onMark}>
            <Icon name="message-square-plus" size={14} /> Mark
          </button>
        </div>
      )}
      {!picked && list.length === 0 && (
        <p className="px-3 text-xs opacity-60">Select a passage in Review to mark it. The agent sees your marks and answers them.</p>
      )}
      <ul className="flex-1 space-y-2 overflow-auto px-2 pb-2">
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
    <li className={`rounded border border-black/10 p-2 text-xs dark:border-white/10 ${mark.status !== "open" ? "opacity-60" : ""}`}>
      {mark.quote && <div className="mb-1 line-clamp-2 italic opacity-70">“{mark.quote}”</div>}
      <div>
        <span className="font-medium">{mark.author}</span>: {mark.body}
      </div>
      {mark.replies?.map((r, i) => (
        <div key={i} className="mt-1 border-l-2 border-black/10 pl-2 dark:border-white/15">
          <span className="font-medium">{r.author}</span>: {r.body}
        </div>
      ))}
      {mark.status === "open" && (
        <div className="mt-1 flex gap-1">
          <input
            className="min-w-0 flex-1 rounded border border-black/15 bg-transparent px-1 dark:border-white/15"
            placeholder="Reply"
            value={reply}
            onChange={(e) => setReply(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void send()
            }}
          />
          <button type="button" className={btn} title="Resolve" onClick={() => void resolveMark(mark.id)}>
            <Icon name="check" size={14} />
          </button>
        </div>
      )}
    </li>
  )
}

const web: ModuleWeb = { views: { "drafts/main": DraftsView } }
export default web
