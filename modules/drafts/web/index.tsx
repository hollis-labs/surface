/**
 * Drafts: documents the owner and the agent work on together. Review shows the text by line:
 * select a passage and mark it. Edit changes the text; every save is a revision. Compare shows
 * any revision against the latest. The marks on the draft are listed beside the text (under
 * it when the window is narrow).
 */
import {
  ApiError,
  addMark,
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
import "./drafts.css"

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
const MODES: { mode: Mode; icon: string }[] = [
  { mode: "review", icon: "scan-text" },
  { mode: "edit", icon: "pencil" },
  { mode: "compare", icon: "git-compare" },
]

function when(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })
}

function DraftsView({ win, desk, api }: ViewProps) {
  const id = typeof win.args.id === "string" ? win.args.id : ""
  return (
    <div className="dr">
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
      <div className="dr-find">
        <input
          className="sf-input sf-grow"
          placeholder="Find a draft"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <button type="button" className="sf-btn" onClick={() => void create()}>
          <Icon name="file-plus-2" size={14} /> New
        </button>
      </div>
      <ul className="dr-list">
        {drafts.map((d) => (
          <li key={d.id}>
            <button type="button" className="dr-row" onClick={() => show(d)}>
              <span className="dr-row-title">{d.title}</span>
              <span className="dr-row-meta">
                {d.lang} · r{d.rev} · {d.lines ?? 0} lines · {when(d.updated)}
              </span>
            </button>
          </li>
        ))}
        {drafts.length === 0 && <li className="dr-hint">No drafts yet.</li>}
      </ul>
    </>
  )
}

function DraftEditor({ id, win, desk, api }: ViewProps & { id: string }) {
  const [draft, setDraft] = useState<Draft | null>(null)
  const [mode, setMode] = useState<Mode>((win.state?.mode as Mode) || "review")
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

  if (!draft) return <p className="dr-hint">{notice || "Loading…"}</p>
  const lines = (draft.body ?? "").replace(/\n$/, "").split("\n")
  const marked = new Set<number>()
  for (const m of marks) {
    const l = (m.anchor as { lines?: { start: number; end?: number } } | undefined)?.lines
    if (m.status === "open" && l) for (let i = l.start; i <= (l.end ?? l.start); i++) marked.add(i)
  }

  return (
    <>
      <div className="dr-bar">
        <button type="button" className="dr-tool" title="All drafts" onClick={() => void desk.setArgs(win.id, {})}>
          <Icon name="list" size={14} />
        </button>
        {mode === "edit" ? (
          <input
            className="sf-input dr-title-input"
            value={title}
            aria-label="Title"
            onChange={(e) => {
              setTitle(e.target.value)
              markDirty()
            }}
          />
        ) : (
          <span className="dr-title">{draft.title}</span>
        )}
        <span className="dr-meta">
          {draft.lang} · r{draft.rev}
        </span>
        {MODES.map(({ mode: m, icon }) => (
          <button
            key={m}
            type="button"
            className="dr-tool"
            data-on={mode === m}
            title={m}
            onClick={() => switchMode(m)}
          >
            <Icon name={icon} size={14} />
            <span className="dr-tool-label">{m}</span>
          </button>
        ))}
        {mode === "edit" && (
          <button type="button" className="dr-tool" disabled={!dirty} title="Save (⌘S)" onClick={() => void save()}>
            <Icon name="save" size={14} />
            <span className="dr-tool-label">Save</span>
          </button>
        )}
      </div>
      {notice && (
        <div className="dr-notice">
          <span className="sf-grow">{notice}</span>
          <button type="button" className="sf-btn" onClick={() => void load()}>
            Reload
          </button>
        </div>
      )}
      <div className="dr-main">
        <div className="dr-body">
          {mode === "review" && (
            <div ref={body} className="dr-lines" onMouseUp={onSelect} onKeyUp={onSelect}>
              {lines.map((l, i) => (
                // biome-ignore lint/suspicious/noArrayIndexKey: a line is its position
                <div key={i} data-line={i + 1} className="dr-line" data-marked={marked.has(i + 1)}>
                  <span className="dr-num">{i + 1}</span>
                  <span className="dr-text">{l || " "}</span>
                </div>
              ))}
            </div>
          )}
          {mode === "edit" && (
            <textarea
              className="dr-edit"
              value={text}
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
              <div className="dr-compare-head">
                <span>Revision</span>
                <select
                  className="sf-input"
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
                <span>against the latest (r{draft.rev})</span>
              </div>
              <div className="dr-diff">
                {draft.rev === 1 && <p className="dr-hint">There is only one revision.</p>}
                {diff.map((d, i) => (
                  <div
                    // biome-ignore lint/suspicious/noArrayIndexKey: a diff line is its position
                    key={i}
                    className="dr-diff-line"
                    data-op={d.op}
                  >
                    <span className="dr-diff-op">{d.op} </span>
                    {d.text || " "}
                  </div>
                ))}
              </div>
            </>
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
    </>
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
  const open = marks.filter((m) => m.status === "open").length
  return (
    <aside className="dr-marks">
      <div className="dr-marks-head">
        <span>Marks ({open} open)</span>
        <label>
          <input type="checkbox" checked={showDone} onChange={(e) => setShowDone(e.target.checked)} /> resolved
        </label>
      </div>
      {picked && (
        <div className="dr-compose">
          <div className="dr-quote">
            Lines {picked.start}
            {picked.end !== picked.start ? `–${picked.end}` : ""}: “{picked.quote}”
          </div>
          <textarea
            placeholder="A comment, a question, a change to make"
            aria-label="Mark"
            value={composing}
            onChange={(e) => setComposing(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") onMark()
            }}
          />
          <div>
            <button type="button" className="sf-btn" data-kind="primary" disabled={!composing.trim()} onClick={onMark}>
              <Icon name="message-square-plus" size={14} /> Mark
            </button>
          </div>
        </div>
      )}
      {!picked && list.length === 0 && (
        <p className="dr-hint">Select a passage in Review to mark it. The agent sees your marks and answers them.</p>
      )}
      <ul className="dr-marks-list">
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
    <li className="dr-mark" data-status={mark.status}>
      {mark.quote && <div className="dr-quote">“{mark.quote}”</div>}
      <div>
        <span className="dr-who">{mark.author}</span>: {mark.body}
      </div>
      {mark.replies?.map((r) => (
        <div key={`${r.created}-${r.author}-${r.body.slice(0, 16)}`} className="dr-reply">
          <span className="dr-who">{r.author}</span>: {r.body}
        </div>
      ))}
      {mark.status === "open" && (
        <div className="dr-mark-act">
          <input
            placeholder="Reply"
            aria-label="Reply"
            value={reply}
            onChange={(e) => setReply(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void send()
            }}
          />
          <button type="button" className="dr-tool" title="Resolve" onClick={() => void resolveMark(mark.id)}>
            <Icon name="check" size={14} />
          </button>
        </div>
      )}
    </li>
  )
}

const web: ModuleWeb = { views: { "drafts/main": DraftsView } }
export default web
