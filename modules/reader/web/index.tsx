/**
 * Reader's window: the entries of the feeds the owner follows, and one entry to read. The list
 * and the reading pane sit side by side when the window is wide; narrower, the entry takes the
 * window and Back returns to the list. Entries are plain text: a feed's markup and scripts are
 * never shown. The text is set by the registry's Markdown, escaped so it reads as written (its
 * web addresses become links). Keys: j/k next and previous, m read or unread, s star, o the original.
 */
import { Icon, type ModuleWeb, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Item, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item"
import { Markdown } from "@/components/ui/markdown"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

const URL_RE = /(https?:\/\/[^\s<>]+|www\.[^\s<>]+)/g

/**
 * An entry's plain text as Markdown that draws it as written: paragraphs kept, a feed's own line
 * breaks inside a paragraph read as wrapping, and every Markdown mark escaped except inside web
 * addresses (which become links).
 */
function plainToMarkdown(text: string): string {
  const escapeMarks = (t: string) =>
    t.replace(/[\\`*_{}[\]<>#+\-!|~&]/g, (c) => `\\${c}`).replace(/^(\d+)([.)])/, "$1\\$2")
  return text
    .split(/\n{2,}/)
    .map((p) =>
      p
        .replace(/\s*\n\s*/g, " ")
        .trim()
        .split(URL_RE)
        .map((part, i) => (i % 2 ? part : escapeMarks(part)))
        .join(""),
    )
    .filter(Boolean)
    .join("\n\n")
}

interface Feed {
  id: string
  url: string
  title: string
  site?: string
  folder?: string
  fetched_at: string
  error?: string
  unread: number
}
interface Entry {
  id: string
  feed_id: string
  url: string
  title: string
  author?: string
  published: string
  text: string
  read: boolean
  starred: boolean
  feed?: string
}
type Show = "unread" | "all" | "starred"
const ALL = "__all"

function ago(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ""
  const m = Math.round((Date.now() - t) / 60000)
  if (m < 60) return `${Math.max(m, 1)} min`
  if (m < 60 * 24) return `${Math.round(m / 60)} h`
  if (m < 60 * 24 * 30) return `${Math.round(m / 1440)} d`
  return new Date(t).toLocaleDateString()
}

function Reader({ win, desk, api }: ViewProps) {
  const [feeds, setFeeds] = useState<Feed[]>([])
  const [entries, setEntries] = useState<Entry[]>([])
  const [show, setShow] = useState<Show>("unread")
  const [feed, setFeed] = useState(ALL)
  const [q, setQ] = useState("")
  const [open, setOpen] = useState<Entry | null>(null)
  const [more, setMore] = useState(false)
  const [busy, setBusy] = useState("")
  const [error, setError] = useState("")
  const [adding, setAdding] = useState(false)
  const pane = useRef<HTMLDivElement>(null)

  const fail = useCallback((e: unknown) => setError(e instanceof Error ? e.message : String(e)), [])
  const loadFeeds = useCallback(
    () =>
      api
        .get<{ feeds: Feed[] }>("feeds")
        .then((r) => setFeeds(r.feeds))
        .catch(fail),
    [api, fail],
  )
  const query = useCallback(
    (before = "") => {
      const p = new URLSearchParams()
      if (feed !== ALL) p.set("feed", feed)
      if (show === "unread") p.set("unread", "1")
      if (show === "starred") p.set("starred", "1")
      if (q.trim()) p.set("q", q.trim())
      if (before) p.set("before", before)
      return `entries?${p.toString()}`
    },
    [feed, show, q],
  )
  const loadEntries = useCallback(
    (before = "") =>
      api
        .get<{ entries: Entry[] }>(query(before))
        .then((r) => {
          setEntries((old) => (before ? [...old, ...r.entries] : r.entries))
          setMore(r.entries.length >= 60)
          setError("")
        })
        .catch(fail),
    [api, query, fail],
  )
  useEffect(() => void loadFeeds(), [loadFeeds])
  useEffect(() => void loadEntries(), [loadEntries])
  useEvent("reader.changed", () => {
    void loadFeeds()
    // A refresh brings new entries; marking is applied in place below.
    void loadEntries()
  })
  useEffect(() => desk.report(win.id, { kind: "data", ref: "reader:entries" }), [desk, win.id])

  const unread = useMemo(() => feeds.reduce((n, f) => n + f.unread, 0), [feeds])
  const current = feeds.find((f) => f.id === feed)

  const mark = useCallback(
    (e: Entry, change: { read?: boolean; starred?: boolean }) => {
      const next = { ...e, ...change }
      setEntries((list) => list.map((x) => (x.id === e.id ? { ...x, ...change } : x)))
      setOpen((o) => (o?.id === e.id ? { ...o, ...change } : o))
      void api.put(`entries/${e.id}`, change).then(loadFeeds).catch(fail)
      return next
    },
    [api, loadFeeds, fail],
  )
  const read = useCallback(
    (e: Entry) => {
      api
        .get<Entry>(`entries/${e.id}`)
        .then((whole) => {
          setOpen({ ...whole, read: true })
          // The pane takes the focus: the list may hide, and the keys keep working.
          window.setTimeout(() => {
            pane.current?.scrollTo({ top: 0 })
            pane.current?.focus({ preventScroll: true })
          }, 0)
          if (!e.read) mark(e, { read: true })
        })
        .catch(fail)
    },
    [api, mark, fail],
  )
  const step = (d: number) => {
    const i = open ? entries.findIndex((e) => e.id === open.id) : -1
    const next = entries[i + d]
    if (next) read(next)
  }
  // The keys work while this window is in front, wherever the focus is (not in a field, not in
  // a dialog). The latest state is read through a ref, so the listener is added once per focus.
  const keys = useRef<(ev: globalThis.KeyboardEvent) => void>(() => {})
  keys.current = (ev) => {
    const t = ev.target as HTMLElement | null
    if (
      ev.metaKey ||
      ev.ctrlKey ||
      ev.altKey ||
      t?.closest("input, textarea, select, [contenteditable=true], [role=dialog]")
    )
      return
    if (ev.key === "j") step(1)
    else if (ev.key === "k") step(-1)
    else if (ev.key === "m" && open) mark(open, { read: !open.read })
    else if (ev.key === "s" && open) mark(open, { starred: !open.starred })
    else if (ev.key === "o" && open?.url) window.open(open.url, "_blank", "noopener,noreferrer")
    else return
    ev.preventDefault()
  }
  useEffect(() => {
    if (!win.focused) return
    const on = (ev: globalThis.KeyboardEvent) => keys.current(ev)
    window.addEventListener("keydown", on)
    return () => window.removeEventListener("keydown", on)
  }, [win.focused])
  const refresh = () => {
    setBusy("refresh")
    api
      .post("feeds/refresh", { feed: feed === ALL ? "" : feed })
      .then(() => Promise.all([loadFeeds(), loadEntries()]))
      .catch(fail)
      .finally(() => setBusy(""))
  }
  const readAll = async () => {
    const ok = await desk.confirm({
      title: current ? `Mark everything in ${current.title} read?` : "Mark every entry read?",
      confirmLabel: "Mark read",
    })
    if (!ok) return
    api
      .post("feeds/read", { feed: feed === ALL ? "" : feed })
      .then(() => Promise.all([loadFeeds(), loadEntries()]))
      .catch(fail)
  }
  const unfollow = async () => {
    if (!current) return
    const ok = await desk.confirm({
      title: `Stop following ${current.title}?`,
      message: "Its entries are forgotten, starred ones too.",
      confirmLabel: "Stop following",
      danger: true,
    })
    if (!ok) return
    api
      .del(`feeds/${current.id}`)
      .then(() => {
        setFeed(ALL)
        return loadFeeds()
      })
      .catch(fail)
  }

  return (
    <div className="@container flex h-full min-h-0 flex-col bg-background">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-2">
        <ToggleGroup
          size="sm"
          spacing={0}
          value={[show]}
          onValueChange={(v: string[]) => v[0] && setShow(v[0] as Show)}
        >
          <ToggleGroupItem value="unread">Unread{unread ? ` ${unread}` : ""}</ToggleGroupItem>
          <ToggleGroupItem value="all">All</ToggleGroupItem>
          <ToggleGroupItem value="starred">Starred</ToggleGroupItem>
        </ToggleGroup>
        <Select
          value={feed}
          onValueChange={(v) => setFeed(String(v))}
          items={[{ value: ALL, label: "Every feed" }, ...feeds.map((f) => ({ value: f.id, label: f.title }))]}
        >
          <SelectTrigger size="sm" aria-label="Feed" className="max-w-64">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>Every feed</SelectItem>
            {feeds.map((f) => (
              <SelectItem key={f.id} value={f.id}>
                {f.folder ? `${f.folder} / ` : ""}
                {f.title}
                {f.unread ? ` (${f.unread})` : ""}
                {f.error ? " ⚠" : ""}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          className="h-8 w-44"
          placeholder="Search"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          aria-label="Search the entries"
        />
        <div className="ml-auto flex flex-wrap items-center gap-1">
          <Button size="sm" variant="ghost" disabled={busy !== ""} onClick={refresh} title="Fetch now">
            <Icon name="refresh-cw" />
          </Button>
          <Button size="sm" variant="ghost" onClick={() => void readAll()} title="Mark all read">
            <Icon name="check-check" />
          </Button>
          {current && (
            <Button size="sm" variant="ghost" onClick={() => void unfollow()} title="Stop following this feed">
              <Icon name="trash-2" />
            </Button>
          )}
          <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
            <Icon name="plus" />
            Feed
          </Button>
        </div>
      </div>
      {(error || current?.error) && (
        <p className="border-b px-3 py-1 text-xs text-destructive">{error || `${current?.title}: ${current?.error}`}</p>
      )}
      <div className="flex min-h-0 flex-1">
        <div
          className={`min-h-0 flex-col overflow-auto border-r p-2 ${open ? "hidden @3xl:flex @3xl:w-96 @3xl:shrink-0" : "flex flex-1"}`}
        >
          {entries.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyTitle>
                  {feeds.length === 0 ? "No feeds yet" : show === "unread" ? "All read" : "Nothing here"}
                </EmptyTitle>
                <EmptyDescription>
                  {feeds.length === 0
                    ? "Follow one with + Feed: a feed's address, or a site that names one."
                    : "Try All, or another feed."}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="flex flex-col gap-0.5">
              {entries.map((e) => (
                <Item
                  key={e.id}
                  size="sm"
                  variant={open?.id === e.id ? "muted" : "default"}
                  render={<a href={`#${e.id}`} />}
                  onClick={(ev) => {
                    ev.preventDefault()
                    read(e)
                  }}
                >
                  <ItemContent className="min-w-0">
                    <ItemTitle className={e.read ? "font-normal text-muted-foreground" : ""}>
                      {!e.read && <span className="size-1.5 shrink-0 rounded-full bg-primary" title="unread" />}
                      <span className="line-clamp-2">{e.title || e.url}</span>
                      {e.starred && <Icon name="star" className="size-3 shrink-0 text-warning" />}
                    </ItemTitle>
                    <ItemDescription className="truncate text-xs">
                      {e.feed} · {ago(e.published)}
                    </ItemDescription>
                  </ItemContent>
                </Item>
              ))}
              {more && (
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    const last = entries[entries.length - 1]
                    void loadEntries(last ? `${last.published}|${last.id}` : "")
                  }}
                >
                  Older
                </Button>
              )}
            </div>
          )}
        </div>
        {open && (
          <article ref={pane} tabIndex={-1} className="min-h-0 min-w-0 flex-1 overflow-auto outline-none">
            <div className="mx-auto flex max-w-3xl flex-col gap-4 px-6 py-5">
              <div className="flex flex-wrap items-center gap-1">
                <Button size="sm" variant="ghost" onClick={() => setOpen(null)} title="Back to the list">
                  <Icon name="arrow-left" />
                  Back
                </Button>
                <div className="ml-auto flex items-center gap-1">
                  <Button
                    size="sm"
                    variant="ghost"
                    aria-pressed={open.starred}
                    onClick={() => mark(open, { starred: !open.starred })}
                    title="Star (s)"
                  >
                    <Icon name="star" className={open.starred ? "text-warning" : ""} />
                    {open.starred ? "Starred" : "Star"}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => mark(open, { read: !open.read })}
                    title="Read or unread (m)"
                  >
                    <Icon name={open.read ? "mail" : "mail-open"} />
                    {open.read ? "Mark unread" : "Mark read"}
                  </Button>
                  {open.url && (
                    <Button
                      size="sm"
                      variant="ghost"
                      nativeButton={false}
                      render={<a href={open.url} target="_blank" rel="noopener noreferrer" />}
                      title="The original (o)"
                    >
                      <Icon name="external-link" />
                      Original
                    </Button>
                  )}
                </div>
              </div>
              <header className="flex flex-col gap-1">
                <h2 className="text-xl font-semibold leading-snug">{open.title || open.url}</h2>
                <p className="text-xs text-muted-foreground">
                  {open.feed}
                  {open.author ? ` · ${open.author}` : ""} · {new Date(open.published).toLocaleString()}
                </p>
                {open.starred && (
                  <div>
                    <Badge variant="outline">starred</Badge>
                  </div>
                )}
              </header>
              {open.text ? (
                <Markdown size="document">{plainToMarkdown(open.text)}</Markdown>
              ) : (
                <p className="text-muted-foreground">This entry has no text. The original has it.</p>
              )}
            </div>
          </article>
        )}
      </div>
      <AddFeed
        open={adding}
        onClose={() => setAdding(false)}
        onAdd={(url, folder) =>
          api.post<Feed>("feeds", { url, folder }).then((f) => {
            setAdding(false)
            setFeed(f.id)
            setShow("all")
            return loadFeeds()
          })
        }
      />
    </div>
  )
}

function AddFeed({
  open,
  onClose,
  onAdd,
}: {
  open: boolean
  onClose: () => void
  onAdd: (url: string, folder: string) => Promise<unknown>
}) {
  const [url, setUrl] = useState("")
  const [folder, setFolder] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const submit = () => {
    if (!url.trim()) return
    setBusy(true)
    setError("")
    onAdd(url.trim(), folder.trim())
      .then(() => {
        setUrl("")
        setFolder("")
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Follow a feed</DialogTitle>
          <DialogDescription>A feed's address, or a site's: the feed it names is followed.</DialogDescription>
        </DialogHeader>
        <Input
          autoFocus
          placeholder="example.test/feed.xml"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
        <Input
          placeholder="Folder (optional)"
          value={folder}
          onChange={(e) => setFolder(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
        />
        {error && <p className="text-xs text-destructive">{error}</p>}
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button disabled={busy || !url.trim()} onClick={submit}>
            Follow
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export default { views: { "reader/main": Reader } } satisfies ModuleWeb
