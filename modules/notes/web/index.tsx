/**
 * Notes: the list of notes on the left, the note in the middle (the Markdown, its preview,
 * or both), and the marks on it on the right; a narrow window stacks them. The text is saved
 * as you write; every save names the revision it started from, so a change the agent made
 * meanwhile is never lost.
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
} from "@hollis-labs/surface-page/sdk"
import { type ComponentProps, type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react"
import Markdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader } from "@/components/ui/card"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Empty, EmptyContent, EmptyDescription, EmptyHeader } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from "@/components/ui/input-group"
import { Item, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item"
import { TextEditor } from "@/components/ui/text-editor"
import { Textarea } from "@/components/ui/textarea"
import { Toggle } from "@/components/ui/toggle"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
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

/** A lone icon in a toolbar, with what it does shown on hover. */
function IconButton({ label, children, ...props }: { label: string } & ComponentProps<typeof Button>) {
  return (
    <Tooltip>
      <TooltipTrigger render={<Button variant="ghost" size="icon-sm" aria-label={label} {...props} />}>
        {children}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

/** One row of the list: a note, or a line of a note the search found. */
function NoteRow({ title, about, on, onOpen }: { title: string; about: string; on?: boolean; onOpen: () => void }) {
  return (
    <Item
      size="xs"
      variant={on ? "muted" : "default"}
      aria-current={on ? "true" : undefined}
      render={
        <a
          href={`#note:${encodeURIComponent(about)}`}
          onClick={(e) => {
            e.preventDefault()
            onOpen()
          }}
        />
      }
    >
      <ItemContent className="min-w-0">
        <ItemTitle className="max-w-full">
          <span className="truncate">{title}</span>
        </ItemTitle>
        <ItemDescription className="truncate">{about}</ItemDescription>
      </ItemContent>
    </Item>
  )
}

/** What a question dialog asks for: one line (a name) or a few (a note). */
interface Question {
  title: string
  about?: string
  initial: string
  placeholder?: string
  confirmLabel: string
  multiline?: boolean
  answer: (text: string) => void
}

function AskForm({ q, onClose }: { q: Question; onClose: () => void }) {
  const [text, setText] = useState(q.initial)
  const submit = (e?: FormEvent) => {
    e?.preventDefault()
    const t = text.trim()
    if (!t) return
    onClose()
    q.answer(t)
  }
  return (
    <form className="grid gap-4" onSubmit={submit}>
      <DialogHeader>
        <DialogTitle>{q.title}</DialogTitle>
        {q.about && <DialogDescription>{q.about}</DialogDescription>}
      </DialogHeader>
      {q.multiline ? (
        <Textarea
          autoFocus
          aria-label={q.title}
          placeholder={q.placeholder}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            // Enter keeps the note, as it did in a one-line box; Shift+Enter starts a new line.
            if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) submit(e)
          }}
        />
      ) : (
        <Input
          autoFocus
          aria-label={q.title}
          placeholder={q.placeholder}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onFocus={(e) => e.target.select()}
        />
      )}
      <DialogFooter>
        <DialogClose render={<Button type="button" variant="outline" />}>Cancel</DialogClose>
        <Button type="submit" disabled={!text.trim()}>
          {q.confirmLabel}
        </Button>
      </DialogFooter>
    </form>
  )
}

/** A small dialog that asks for a line or a few of text. */
function Ask({ q, onClose }: { q: Question | null; onClose: () => void }) {
  // The last question stays drawn while the dialog closes.
  const last = useRef<Question | null>(q)
  if (q) last.current = q
  const shown = q ?? last.current
  return (
    <Dialog
      open={!!q}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <DialogContent showCloseButton={false}>{shown && <AskForm q={shown} onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

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
    <div className="@container/notes h-full min-h-0 bg-background text-sm text-foreground">
      {/* A narrow window puts the list above the note instead of beside it. */}
      <div className="flex h-full min-h-0 flex-col @lg/notes:flex-row">
        <aside className="flex max-h-48 min-h-0 shrink-0 flex-col border-b border-border bg-card @lg/notes:max-h-none @lg/notes:w-44 @lg/notes:border-r @lg/notes:border-b-0 @2xl/notes:w-52">
          <div className="p-2">
            <InputGroup>
              <InputGroupInput
                placeholder="Find or name a new note…"
                aria-label="Find or name a new note"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && shown.length === 0) void create()
                  else if (e.key === "Enter" && shown.length === 1) open(shown[0].path)
                }}
              />
              <InputGroupAddon align="inline-end">
                <InputGroupButton size="icon-xs" title="New note" aria-label="New note" onClick={() => void create()}>
                  <Icon name="plus" />
                </InputGroupButton>
              </InputGroupAddon>
            </InputGroup>
          </div>
          <div className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-auto px-1 pb-1.5">
            {shown.map((n) => (
              <NoteRow key={n.path} title={n.title} about={n.path} on={n.path === path} onOpen={() => open(n.path)} />
            ))}
            {hits && hits.length > 0 && (
              <div className="px-2.5 pt-2.5 pb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">
                In the text
              </div>
            )}
            {hits?.map((h) => (
              <NoteRow
                key={`${h.path}:${h.line}`}
                title={h.text}
                about={`${h.path}:${h.line}`}
                onOpen={() => open(h.path)}
              />
            ))}
            {shown.length === 0 && !hits?.length && (
              <p className="px-2.5 py-3.5 text-muted-foreground">
                {query ? "No note matches. Enter makes one." : "No notes yet."}
              </p>
            )}
          </div>
          <footer
            className="hidden truncate border-t border-border px-2.5 py-1.5 text-left text-xs text-muted-foreground [direction:rtl] @lg/notes:block"
            title={folder}
          >
            {folder}
          </footer>
        </aside>
        {error && !path ? (
          <Empty>
            <EmptyHeader>
              <EmptyDescription className="text-destructive">{error}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : path ? (
          <Editor key={path} path={path} win={win} desk={desk} api={api} onOpen={open} onCreate={create} />
        ) : (
          <Empty>
            <EmptyHeader>
              <EmptyDescription>Pick a note, or type a name and press Enter to start one.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
      </div>
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
  const [asking, setAsking] = useState<Question | null>(null)
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

  const markSelection = () => {
    const quote = selection || window.getSelection()?.toString().trim() || ""
    setAsking({
      title: "Mark",
      about: quote ? `Note on “${quote.slice(0, 80)}”` : "Note on this note",
      initial: "",
      placeholder: "What to say about it…",
      confirmLabel: "Add mark",
      multiline: true,
      answer: (body) => {
        void addMark({ ref, quote: quote || undefined, body }).then(() => setShowMarks(true))
      },
    })
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

  const renameTo = async (to: string) => {
    if (to === path.replace(/\.md$/, "")) return
    await save()
    try {
      const n = await api.post<Note>("rename", { from: path, to })
      onOpen(n.path)
    } catch (e) {
      setError((e as Error).message)
    }
  }
  const rename = () =>
    setAsking({
      title: "Rename the note",
      about: "A name, or a path like folder/name.",
      initial: path.replace(/\.md$/, ""),
      confirmLabel: "Rename",
      answer: (to) => void renameTo(to),
    })

  const follow = (name: string) => {
    const target = loaded?.links[name]
    if (target) onOpen(target)
    else void onCreate(name)
  }

  if (status === "missing") {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyDescription>There is no note at {path}.</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button onClick={() => void onCreate(path.replace(/\.md$/, ""))}>Create it</Button>
        </EmptyContent>
      </Empty>
    )
  }

  return (
    <div className="@container/main flex min-h-0 min-w-0 flex-1 flex-col">
      <header className="flex shrink-0 items-center gap-1 border-b border-border py-1.5 pr-2 pl-3">
        <span className="min-w-0 truncate font-mono text-xs text-muted-foreground" title={path}>
          {path}
        </span>
        <span
          className={`ml-1 shrink-0 text-xs ${status === "conflict" ? "text-warning" : "text-muted-foreground"}`}
          data-status={status}
        >
          {status === "saved"
            ? "saved"
            : status === "saving"
              ? "saving…"
              : status === "dirty"
                ? "…"
                : "changed elsewhere"}
        </span>
        <span className="flex-1" />
        <Tooltip>
          <TooltipTrigger
            render={<Button variant="ghost" size="sm" aria-label="Mark" onClick={() => void markSelection()} />}
          >
            <Icon name="message-square-quote" />
            <span className="hidden @md/main:inline">Mark</span>
          </TooltipTrigger>
          <TooltipContent>Pin a note to the selection</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger
            render={
              <Toggle
                size="sm"
                aria-label="Show or hide the marks"
                pressed={showMarks}
                onPressedChange={(on) => setShowMarks(on)}
              />
            }
          >
            <Icon name="messages-square" />
            {openMarks.length}
          </TooltipTrigger>
          <TooltipContent>Show or hide the marks</TooltipContent>
        </Tooltip>
        <IconButton label="Rename" onClick={rename}>
          <Icon name="pencil-line" />
        </IconButton>
        <IconButton label="Delete" variant="destructive" onClick={() => void remove()}>
          <Icon name="trash" />
        </IconButton>
      </header>
      {status === "conflict" && (
        <div className="flex flex-wrap items-center gap-x-2 bg-warning/10 px-3 py-1 text-xs text-warning">
          This note was changed elsewhere while you were writing.
          <Button variant="link" size="xs" onClick={() => load(true)}>
            take that version
          </Button>
          <Button
            variant="link"
            size="xs"
            onClick={() => {
              revRef.current = loaded?.note.rev ?? ""
              setStatus("dirty")
            }}
          >
            keep mine
          </Button>
        </div>
      )}
      {error && <p className="px-3 py-1.5 text-destructive">{error}</p>}
      {/* A narrow window stacks the text, the preview and the marks instead of squeezing them. */}
      <div className="flex min-h-0 flex-1 flex-col @3xl/main:flex-row" data-layout={layout}>
        <div className="flex min-h-0 min-w-0 flex-1 flex-col divide-y divide-border @2xl/main:flex-row @2xl/main:divide-x @2xl/main:divide-y-0">
          {layout !== "preview" && (
            <div className="min-h-0 min-w-0 flex-1 basis-0">
              <TextEditor
                ref={area}
                value={text}
                spellCheck
                aria-label="The note's Markdown"
                onChange={(e) => change(e.target.value)}
                onSelect={onSelect}
                onBlur={() => void save()}
                placeholder="Write in Markdown. Link notes with [[name]]."
              />
            </div>
          )}
          {layout !== "edit" && (
            <article className="notes-preview min-h-0 min-w-0 flex-1 basis-0 overflow-auto px-5 pt-3.5 pb-8">
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
                <footer className="mt-6 flex flex-wrap items-center gap-x-1 gap-y-0.5 border-t border-border pt-2.5">
                  <span className="text-xs text-muted-foreground">Linked from</span>
                  {loaded.backlinks.map((b) => (
                    <Button key={b.path} variant="link" size="xs" onClick={() => onOpen(b.path)}>
                      {b.title}
                    </Button>
                  ))}
                </footer>
              )}
            </article>
          )}
        </div>
        {showMarks && openMarks.length > 0 && (
          <aside className="flex max-h-1/3 shrink-0 flex-col gap-2 overflow-auto border-t border-border bg-muted/50 p-2 @3xl/main:max-h-none @3xl/main:w-64 @3xl/main:border-t-0 @3xl/main:border-l">
            {openMarks.map((m) => (
              <MarkNote key={m.id} mark={m} />
            ))}
          </aside>
        )}
      </div>
      <Ask q={asking} onClose={() => setAsking(null)} />
    </div>
  )
}

function MarkNote({ mark }: { mark: Mark }) {
  const [reply, setReply] = useState("")
  return (
    <Card size="sm" className="shrink-0" data-author={mark.author}>
      <CardHeader>
        <CardDescription className="flex items-center gap-1.5">
          <Icon name={mark.author === "agent" ? "bot" : "user"} size={14} />
          {mark.author === "agent" ? "Agent" : "You"}
        </CardDescription>
        <CardAction>
          <Button variant="link" size="xs" onClick={() => void resolveMark(mark.id)}>
            resolve
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        {mark.quote && (
          <blockquote className="border-l-2 border-border pl-2 text-muted-foreground">{mark.quote}</blockquote>
        )}
        <p className="wrap-anywhere whitespace-pre-wrap">{mark.body}</p>
        {mark.replies?.map((r) => (
          <p key={r.created + r.body} className="flex items-start gap-1.5 text-muted-foreground">
            <Icon name={r.author === "agent" ? "bot" : "user"} size={12} className="mt-1 shrink-0" />
            <span className="min-w-0 wrap-anywhere">{r.body}</span>
          </p>
        ))}
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (reply.trim()) void replyMark(mark.id, reply.trim()).then(() => setReply(""))
          }}
        >
          <Input placeholder="Reply…" aria-label="Reply" value={reply} onChange={(e) => setReply(e.target.value)} />
        </form>
      </CardContent>
    </Card>
  )
}

export default { views: { "notes/main": Notes } } satisfies ModuleWeb
