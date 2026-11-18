/**
 * Notes: the list of notes on the left, the note in the middle (the Markdown, its preview,
 * or both), and the marks on it on the right. The text is saved as you write; every save
 * names the revision it started from, so a change the agent made meanwhile is never lost.
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
  useSetting,
  type ViewProps,
} from "@surface/sdk"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import Markdown from "react-markdown"
import remarkGfm from "remark-gfm"
import "./notes.css"

interface Note {
  path: string
  title: string
  text?: string
  rev?: string
  modified: string
}
interface Loaded {
  note: Note
  backlinks: Note[]
  /** [[name]] as written, to the path it leads to. A name that is not here has no note yet. */
  links: Record<string, string>
}
interface Hit {
  path: string
  title: string
  line: number
  text: string
}
type LayoutMode = "split" | "edit" | "preview"

/** [[name]] and [[name|label]] become links the preview can open. */
const wikiToLinks = (text: string) =>
  text.replace(/\[\[([^\]|#]+)(?:\|([^\]]*))?\]\]/g, (_m, name: string, label?: string) => {
    const n = name.trim()
    return `[${(label ?? n).trim()}](#note:${encodeURIComponent(n)})`
  })

function Notes({ win, desk, api }: ViewProps) {
  const [notes, setNotes] = useState<Note[]>([])
  const [folder, setFolder] = useState("")
  const [query, setQuery] = useState("")
  const [hits, setHits] = useState<Hit[] | null>(null)
  const [error, setError] = useState("")
  const path = typeof win.args.path === "string" ? win.args.path : ""

  const loadList = useCallback(() => {
    api
      .get<{ notes: Note[]; folder: string }>("notes")
      .then((r) => {
        setNotes(r.notes)
        setFolder(r.folder)
        setError("")
      })
      .catch((e: Error) => setError(e.message))
  }, [api])
  useEffect(loadList, [loadList])
  useEvent("notes.changed", loadList)

  // A window opened from Files carries a place and a path; ask where that is in the notes folder.
  useEffect(() => {
    const { place, path: p } = win.args as { place?: string; path?: string }
    if (!place || !p) return
    api
      .post<{ path: string }>("locate", { place, path: p })
      .then((r) => desk.setArgs(win.id, { path: r.path }))
      .catch((e: Error) => {
        setError(e.message)
        void desk.setArgs(win.id, {})
      })
  }, [api, desk, win.id, win.args])

  useEffect(() => {
    const q = query.trim()
    if (q.length < 2) {
      setHits(null)
      return
    }
    const t = window.setTimeout(() => {
      api
        .get<Hit[]>(`search?q=${encodeURIComponent(q)}`)
        .then(setHits)
        .catch(() => setHits([]))
    }, 180)
    return () => window.clearTimeout(t)
  }, [api, query])

  const open = useCallback((p: string) => void desk.setArgs(win.id, { path: p }), [desk, win.id])

  const create = async (name?: string) => {
    const title =
      (name ?? query).trim() || `Untitled ${new Date().toISOString().slice(0, 16).replace("T", " ").replace(":", "")}`
    try {
      const n = await api.post<Note>("notes", { path: title, text: `# ${title.split("/").pop()}\n\n` })
      setQuery("")
      open(n.path)
    } catch (e) {
      setError((e as Error).message)
    }
  }

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return notes
    return notes.filter((n) => n.path.toLowerCase().includes(q) || n.title.toLowerCase().includes(q))
  }, [notes, query])

  return (
    <div className="notes">
      <aside className="notes-side">
        <div className="notes-find">
          <input
            className="sf-input sf-grow"
            placeholder="Find or name a new note…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && shown.length === 0) void create()
              else if (e.key === "Enter" && shown.length === 1) open(shown[0].path)
            }}
          />
          <button type="button" className="sf-btn" title="New note" onClick={() => void create()}>
            <Icon name="plus" size={14} />
          </button>
        </div>
        <ul className="notes-list">
          {shown.map((n) => (
            <li key={n.path}>
              <button type="button" className="notes-row" data-on={n.path === path} onClick={() => open(n.path)}>
                <span className="notes-row-title">{n.title}</span>
                <span className="notes-row-path">{n.path}</span>
              </button>
            </li>
          ))}
          {hits && hits.length > 0 && <li className="notes-head">In the text</li>}
          {hits?.map((h) => (
            <li key={`${h.path}:${h.line}`}>
              <button type="button" className="notes-row" onClick={() => open(h.path)}>
                <span className="notes-row-title">{h.text}</span>
                <span className="notes-row-path">
                  {h.path}:{h.line}
                </span>
              </button>
            </li>
          ))}
          {shown.length === 0 && !hits?.length && (
            <li className="sf-dim notes-none">{query ? "No note matches. Enter makes one." : "No notes yet."}</li>
          )}
        </ul>
        <footer className="notes-folder sf-dim" title={folder}>
          {folder}
        </footer>
      </aside>
      {error && !path ? (
        <div className="notes-empty">
          <p className="sf-error">{error}</p>
        </div>
      ) : path ? (
        <Editor key={path} path={path} win={win} desk={desk} api={api} onOpen={open} onCreate={create} />
      ) : (
        <div className="notes-empty sf-dim">
          <p>Pick a note, or type a name and press Enter to start one.</p>
        </div>
      )}
    </div>
  )
}

interface EditorProps extends ViewProps {
  path: string
  onOpen: (path: string) => void
  onCreate: (name: string) => Promise<void>
}

function Editor({ path, win, desk, api, onOpen, onCreate }: EditorProps) {
  const [loaded, setLoaded] = useState<Loaded | null>(null)
  const [text, setText] = useState("")
  const [status, setStatus] = useState<"saved" | "dirty" | "saving" | "conflict" | "missing">("saved")
  const [error, setError] = useState("")
  const [selection, setSelection] = useState("")
  const [showMarks, setShowMarks] = useState(true)
  const layout = (useSetting<LayoutMode>("notes.layout") ?? "split") as LayoutMode
  const revRef = useRef("")
  const textRef = useRef("")
  const dirtyRef = useRef(false)
  const area = useRef<HTMLTextAreaElement>(null)
  const ref = `notes:${path}`
  const marks = useMarks(ref)
  const openMarks = marks.filter((m) => m.status === "open")

  const load = useCallback(
    (force: boolean) => {
      api
        .get<Loaded>(`note?path=${encodeURIComponent(path)}`)
        .then((r) => {
          setLoaded(r)
          // Take the server's text unless the owner has unsaved words of their own.
          if (force || !dirtyRef.current) {
            revRef.current = r.note.rev ?? ""
            textRef.current = r.note.text ?? ""
            setText(textRef.current)
            dirtyRef.current = false
            setStatus("saved")
          } else if (r.note.rev !== revRef.current) {
            setStatus("conflict")
          }
          setError("")
        })
        .catch((e: Error) => {
          if (e instanceof ApiError && e.status === 404) setStatus("missing")
          setError(e.message)
        })
    },
    [api, path],
  )
  useEffect(() => load(true), [load])
  // The agent (or another window) saved this note: show it, unless the owner is mid-edit.
  useEvent<{ path: string }>("notes.changed", (ev) => {
    if (ev.data?.path === path) load(false)
  })

  const save = useCallback(async () => {
    if (!dirtyRef.current) return
    const sending = textRef.current
    setStatus("saving")
    try {
      const n = await api.put<Note>("note", { path, text: sending, rev: revRef.current })
      revRef.current = n.rev ?? ""
      if (textRef.current === sending) {
        dirtyRef.current = false
        setStatus("saved")
      } else {
        setStatus("dirty")
      }
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) setStatus("conflict")
      else setError((e as Error).message)
    }
  }, [api, path])

  // Save a moment after the typing stops, and when the window goes away.
  // biome-ignore lint/correctness/useExhaustiveDependencies: each keystroke restarts the wait
  useEffect(() => {
    if (status !== "dirty") return
    const t = window.setTimeout(() => void save(), 900)
    return () => window.clearTimeout(t)
  }, [status, text, save])
  useEffect(() => () => void save(), [save])

  useEffect(() => {
    desk.setDirty(win.id, status === "dirty" || status === "saving" || status === "conflict")
  }, [desk, win.id, status])

  const title = loaded?.note.title ?? path
  useEffect(() => {
    void desk.setTitle(win.id, title)
  }, [desk, win.id, title])

  // What this window is showing, for the agent and the context slots.
  useEffect(() => {
    desk.report(win.id, { kind: "document", ref, path, selection: selection || undefined })
  }, [desk, win.id, ref, path, selection])

  const onSelect = () => {
    const el = area.current
    if (el) setSelection(el.value.slice(el.selectionStart, el.selectionEnd).trim().slice(0, 2000))
  }

  const change = (v: string) => {
    textRef.current = v
    dirtyRef.current = true
    setText(v)
    if (status !== "conflict") setStatus("dirty")
  }

  const markSelection = async () => {
    const quote = selection || window.getSelection()?.toString().trim() || ""
    const body = window.prompt(quote ? `Note on “${quote.slice(0, 80)}”` : "Note on this note")
    if (!body?.trim()) return
    await addMark({ ref, quote: quote || undefined, body: body.trim() })
    setShowMarks(true)
  }

  const remove = async () => {
    const yes = await desk.confirm({
      title: `Delete “${title}”?`,
      message: "It goes to the notes folder's trash.",
      confirmLabel: "Delete",
      danger: true,
    })
    if (!yes) return
    dirtyRef.current = false
    await api.del(`note?path=${encodeURIComponent(path)}`)
    await desk.setArgs(win.id, {})
  }

  const rename = async () => {
    const to = window.prompt("Rename the note to", path.replace(/\.md$/, ""))
    if (!to?.trim() || to.trim() === path.replace(/\.md$/, "")) return
    await save()
    try {
      const n = await api.post<Note>("rename", { from: path, to: to.trim() })
      onOpen(n.path)
    } catch (e) {
      setError((e as Error).message)
    }
  }

  const follow = (name: string) => {
    const target = loaded?.links[name]
    if (target) onOpen(target)
    else void onCreate(name)
  }

  if (status === "missing") {
    return (
      <div className="notes-empty">
        <p className="sf-dim">There is no note at {path}.</p>
        <button
          type="button"
          className="sf-btn"
          data-kind="primary"
          onClick={() => void onCreate(path.replace(/\.md$/, ""))}
        >
          Create it
        </button>
      </div>
    )
  }

  return (
    <div className="notes-main">
      <header className="notes-bar">
        <span className="notes-path" title={path}>
          {path}
        </span>
        <span className="notes-status" data-status={status}>
          {status === "saved"
            ? "saved"
            : status === "saving"
              ? "saving…"
              : status === "dirty"
                ? "…"
                : "changed elsewhere"}
        </span>
        <span className="sf-grow" />
        <button
          type="button"
          className="sf-btn"
          title="Pin a note to the selection"
          onClick={() => void markSelection()}
        >
          <Icon name="message-square-quote" size={13} />
          Mark
        </button>
        <button
          type="button"
          className="sf-btn"
          data-on={showMarks}
          title="Show or hide the marks"
          onClick={() => setShowMarks((s) => !s)}
        >
          {openMarks.length}
        </button>
        <button type="button" className="sf-btn" title="Rename" onClick={() => void rename()}>
          <Icon name="pencil-line" size={13} />
        </button>
        <button type="button" className="sf-btn" data-kind="danger" title="Delete" onClick={() => void remove()}>
          <Icon name="trash" size={13} />
        </button>
      </header>
      {status === "conflict" && (
        <div className="notes-conflict">
          This note was changed elsewhere while you were writing.
          <button type="button" className="sf-link" onClick={() => load(true)}>
            take that version
          </button>
          <button
            type="button"
            className="sf-link"
            onClick={() => {
              revRef.current = loaded?.note.rev ?? ""
              setStatus("dirty")
            }}
          >
            keep mine
          </button>
        </div>
      )}
      {error && <p className="sf-error notes-error">{error}</p>}
      <div className="notes-body" data-layout={layout}>
        {layout !== "preview" && (
          <textarea
            ref={area}
            className="notes-text"
            value={text}
            spellCheck
            onChange={(e) => change(e.target.value)}
            onSelect={onSelect}
            onBlur={() => void save()}
            placeholder="Write in Markdown. Link notes with [[name]]."
          />
        )}
        {layout !== "edit" && (
          <article className="notes-preview">
            <Markdown
              remarkPlugins={[remarkGfm]}
              urlTransform={(url) => url}
              components={{
                a({ href, children }) {
                  if (href?.startsWith("#note:")) {
                    const name = decodeURIComponent(href.slice(6))
                    return (
                      <a
                        href={href}
                        className="notes-wiki"
                        data-missing={!loaded?.links[name]}
                        title={loaded?.links[name] ?? "No note yet: click to start it"}
                        onClick={(e) => {
                          e.preventDefault()
                          follow(name)
                        }}
                      >
                        {children}
                      </a>
                    )
                  }
                  return (
                    <a
                      href={href}
                      onClick={(e) => {
                        e.preventDefault()
                        if (href)
                          void desk
                            .open("browser", { args: { url: href } })
                            .catch(() => window.open(href, "_blank", "noopener"))
                      }}
                    >
                      {children}
                    </a>
                  )
                },
              }}
            >
              {wikiToLinks(text)}
            </Markdown>
            {loaded && loaded.backlinks.length > 0 && (
              <footer className="notes-back">
                <span className="sf-dim">Linked from</span>
                {loaded.backlinks.map((b) => (
                  <button type="button" key={b.path} className="sf-link" onClick={() => onOpen(b.path)}>
                    {b.title}
                  </button>
                ))}
              </footer>
            )}
          </article>
        )}
        {showMarks && openMarks.length > 0 && (
          <aside className="notes-marks">
            {openMarks.map((m) => (
              <MarkNote key={m.id} mark={m} />
            ))}
          </aside>
        )}
      </div>
    </div>
  )
}

function MarkNote({ mark }: { mark: Mark }) {
  const [reply, setReply] = useState("")
  return (
    <div className="sf-card sf-mark" data-author={mark.author}>
      <div className="sf-card-head">
        <Icon name={mark.author === "agent" ? "bot" : "user"} size={13} />
        <span className="sf-dim">{mark.author === "agent" ? "Agent" : "You"}</span>
        <span className="sf-grow" />
        <button type="button" className="sf-link" onClick={() => void resolveMark(mark.id)}>
          resolve
        </button>
      </div>
      {mark.quote && <blockquote>{mark.quote}</blockquote>}
      <p>{mark.body}</p>
      {mark.replies?.map((r) => (
        <p key={r.created + r.body} className="sf-mark-reply">
          <Icon name={r.author === "agent" ? "bot" : "user"} size={12} /> {r.body}
        </p>
      ))}
      <form
        onSubmit={(e) => {
          e.preventDefault()
          if (reply.trim()) void replyMark(mark.id, reply.trim()).then(() => setReply(""))
        }}
      >
        <input
          className="sf-input notes-reply"
          placeholder="Reply…"
          value={reply}
          onChange={(e) => setReply(e.target.value)}
        />
      </form>
    </div>
  )
}

export default { views: { "notes/main": Notes } } satisfies ModuleWeb
