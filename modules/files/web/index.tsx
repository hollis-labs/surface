/**
 * Files' window: a browser over the owner's places. The place and folder live in the
 * window's args, so they survive a reload and a saved layout, and the agent can take the
 * window somewhere by setting them.
 */
import {
  Icon,
  type ModuleAPI,
  type ModuleWeb,
  useEvent,
  useSetting,
  type ViewProps,
} from "@hollis-labs/surface-page/sdk"
import { type ComponentProps, type FormEvent, useCallback, useEffect, useRef, useState } from "react"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"

interface Place {
  id: string
  name: string
  path: string
}

interface Entry {
  name: string
  /** Relative to the place. */
  path: string
  dir: boolean
  size: number
  modified: string
  symlink?: boolean
  /** Looks like a credential: listed, and nothing else. */
  locked?: boolean
}

interface Listing {
  place: string
  path: string
  entries: Entry[]
}

interface TextPart {
  text: string
  truncated: boolean
  size: number
}

type Naming = { kind: "folder" } | { kind: "rename"; entry: Entry } | null

const IMAGE = /\.(png|jpe?g|gif|webp|avif|bmp|ico|svg)$/i
const NOTE = /\.(md|txt)$/i

const at = (place: string, path: string) => `place=${encodeURIComponent(place)}&path=${encodeURIComponent(path)}`
const join = (dir: string, name: string) => (dir ? `${dir}/${name}` : name)

function formatSize(n: number) {
  if (n < 1024) return `${n} B`
  const units = ["KB", "MB", "GB", "TB"]
  let v = n / 1024
  let u = 0
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024
    u++
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[u]}`
}

function formatTime(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })
}

function iconOf(e: Entry) {
  if (e.locked) return "lock"
  if (e.dir) return "folder"
  if (IMAGE.test(e.name)) return "image"
  return NOTE.test(e.name) ? "file-text" : "file"
}

/** A lone icon in a toolbar or a row, with what it does shown on hover. */
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

/** A one-line name box: Enter keeps the name, Escape or leaving the box gives up. */
function NameBox({
  initial,
  onDone,
  onCancel,
}: {
  initial: string
  onDone: (name: string) => void
  onCancel: () => void
}) {
  const [name, setName] = useState(initial)
  const box = useRef<HTMLInputElement>(null)
  useEffect(() => {
    box.current?.focus()
    box.current?.select()
  }, [])
  const submit = (e: FormEvent) => {
    e.preventDefault()
    const n = name.trim()
    if (!n || n === initial) onCancel()
    else onDone(n)
  }
  return (
    <form className="flex min-w-0 flex-1" onSubmit={submit}>
      <Input
        ref={box}
        className="flex-1"
        value={name}
        aria-label="Name"
        onChange={(e) => setName(e.target.value)}
        onBlur={onCancel}
        onKeyDown={(e) => {
          if (e.key === "Escape") onCancel()
        }}
      />
    </form>
  )
}

/** The form that adds a place: a name and the full path of a folder on this machine. */
function AddPlace({ onAdd, onCancel }: { onAdd: (name: string, path: string) => void; onCancel: () => void }) {
  const [name, setName] = useState("")
  const [path, setPath] = useState("")
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (path.trim()) onAdd(name.trim(), path.trim())
  }
  return (
    <form className="flex flex-wrap items-center gap-2 border-b border-border bg-card px-2.5 py-2" onSubmit={submit}>
      <Input
        className="w-32"
        placeholder="Name"
        value={name}
        aria-label="Place name"
        onChange={(e) => setName(e.target.value)}
      />
      <Input
        className="min-w-40 flex-1"
        placeholder="Folder, like ~/Documents"
        value={path}
        aria-label="Folder path"
        onChange={(e) => setPath(e.target.value)}
      />
      <Button type="submit" disabled={!path.trim()}>
        Add place
      </Button>
      <Button type="button" variant="outline" onClick={onCancel}>
        Cancel
      </Button>
    </form>
  )
}

/** What the selected file holds: an image through the raw route, or the start of its text. */
function Preview({ api, place, entry, limitKb }: { api: ModuleAPI; place: string; entry: Entry; limitKb: number }) {
  const [part, setPart] = useState<TextPart | null>(null)
  const [error, setError] = useState("")
  const image = IMAGE.test(entry.name)
  const stamp = `${entry.modified}:${entry.size}`

  // Read again when the file changes: its listing entry carries a new time or size.
  // biome-ignore lint/correctness/useExhaustiveDependencies: stamp is the reload trigger
  useEffect(() => {
    setPart(null)
    setError("")
    if (image) return
    let live = true
    api
      .get<TextPart>(`read?${at(place, entry.path)}`)
      .then((p) => live && setPart(p))
      .catch((e: Error) => live && setError(e.message))
    return () => {
      live = false
    }
  }, [api, place, entry.path, image, stamp])

  if (image) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center overflow-auto p-3">
        <img
          className="max-h-full max-w-full object-contain"
          src={api.url(`raw?${at(place, entry.path)}&v=${encodeURIComponent(stamp)}`)}
          alt={entry.name}
        />
      </div>
    )
  }
  if (error) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyDescription>No text preview: {error}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  }
  if (!part) return <p className="min-h-0 flex-1 p-3 text-muted-foreground">Reading…</p>
  const about = "border-b border-border bg-card px-3 py-2 text-xs text-muted-foreground"
  return (
    <div className="min-h-0 flex-1 overflow-auto">
      {part.truncated && (
        <p className={about}>
          Showing the first {formatSize(Math.min(limitKb * 1024, part.size))} of {formatSize(part.size)}. The preview
          limit is a Files setting.
        </p>
      )}
      {part.text ? (
        <pre className="m-0 p-3 font-mono text-xs leading-normal wrap-anywhere whitespace-pre-wrap tab-4">
          {part.text}
        </pre>
      ) : (
        <p className={about}>This file is empty.</p>
      )}
    </div>
  )
}

function Files({ win, desk, api }: ViewProps) {
  const place = typeof win.args.place === "string" ? win.args.place : ""
  const path = typeof win.args.path === "string" ? win.args.path : ""
  const select = typeof win.args.select === "string" ? win.args.select : ""
  const showHidden = useSetting<boolean>("files.show_hidden") ?? false
  const limitKb = useSetting<number>("files.preview_limit_kb") ?? 256

  const [places, setPlaces] = useState<Place[] | null>(null)
  const [entries, setEntries] = useState<Entry[]>([])
  const [error, setError] = useState("")
  const [note, setNote] = useState("")
  const [filter, setFilter] = useState("")
  const [selected, setSelected] = useState(select)
  const [naming, setNaming] = useState<Naming>(null)
  const [adding, setAdding] = useState(false)
  const request = useRef(0)

  const fail = (e: Error) => setError(e.message)

  const loadPlaces = useCallback(() => {
    api
      .get<Place[]>("places")
      .then(setPlaces)
      .catch((e: Error) => setError(e.message))
  }, [api])
  useEffect(loadPlaces, [loadPlaces])

  const loadList = useCallback(() => {
    if (!place) return
    // Only the newest request may answer: a slow folder must not replace the one opened after it.
    const mine = ++request.current
    api
      .get<Listing>(`list?${at(place, path)}`)
      .then((l) => {
        if (mine !== request.current) return
        setEntries(l.entries)
        setError("")
      })
      .catch((e: Error) => {
        if (mine !== request.current) return
        setEntries([])
        setError(e.message)
      })
  }, [api, place, path])
  useEffect(loadList, [loadList])

  // The agent, a command or another window changed something on disk, or a place.
  useEvent("files.changed", () => {
    loadPlaces()
    loadList()
  })

  const go = useCallback(
    (toPlace: string, toPath: string) => {
      setSelected("")
      setFilter("")
      setNaming(null)
      setNote("")
      void desk.setArgs(win.id, { place: toPlace, path: toPath })
    },
    [desk, win.id],
  )

  // A window with no place yet, or with one that was removed, starts at the first place.
  useEffect(() => {
    if (!places || places.length === 0) return
    if (!places.some((p) => p.id === place)) go(places[0].id, "")
  }, [places, place, go])

  // The agent opened a file here: its folder is in the args, with the file to select.
  useEffect(() => {
    if (select) setSelected(select)
  }, [select])

  const placeName = places?.find((p) => p.id === place)?.name
  const title = path ? path.slice(path.lastIndexOf("/") + 1) : (placeName ?? "Files")
  useEffect(() => {
    void desk.setTitle(win.id, title)
  }, [desk, win.id, title])

  const current = entries.find((e) => e.path === selected && !e.dir && !e.locked)
  const selection = current?.path

  // What this window is showing, for the agent and the context slots.
  useEffect(() => {
    if (place) desk.report(win.id, { kind: "folder", ref: `files:${place}:${path}`, selection })
  }, [desk, win.id, place, path, selection])

  const needle = filter.trim().toLowerCase()
  const shown = needle ? entries.filter((e) => e.name.toLowerCase().includes(needle)) : entries

  const makeFolder = (name: string) => {
    setNaming(null)
    api.post("mkdir", { place, path: join(path, name) }).catch(fail)
  }
  const rename = (entry: Entry, name: string) => {
    setNaming(null)
    const to = join(path, name)
    api
      .post("move", { place, from: entry.path, to })
      .then(() => {
        if (selected === entry.path) setSelected(to)
      })
      .catch(fail)
  }
  const remove = async (entry: Entry) => {
    const ok = await desk.confirm({
      title: `Delete ${entry.name}?`,
      message: entry.dir
        ? "Only an empty folder can be deleted. This cannot be undone."
        : "The file is deleted for good, not moved to a trash.",
      confirmLabel: "Delete",
      danger: true,
    })
    if (ok) api.post("delete", { place, path: entry.path }).catch(fail)
  }
  const openInNotes = (entry: Entry) => {
    setNote("")
    desk
      .open("notes", { args: { place, path: entry.path }, key: `${place}:${entry.path}` })
      .catch(() => setNote(`${entry.name} was not opened: this app has no Notes, or Notes could not open it.`))
  }
  const addPlace = (name: string, dir: string) => {
    api
      .post<Place>("places", { name, path: dir })
      .then((p) => {
        setAdding(false)
        go(p.id, "")
      })
      .catch(fail)
  }
  const removePlace = async () => {
    const ok = await desk.confirm({
      title: `Remove the place ${placeName ?? place}?`,
      message: "Only the place is removed from Files. The folder and its files stay where they are.",
      confirmLabel: "Remove",
    })
    if (ok) api.del(`places/${encodeURIComponent(place)}`).catch(fail)
  }

  const crumbs = path ? path.split("/") : []

  // The size and the time give way when the list is narrow: it is its own container.
  const sizeCell = "hidden py-1 text-right text-muted-foreground @sm/list:table-cell"
  const timeCell = "hidden truncate py-1 text-muted-foreground @xl/list:table-cell"
  const iconFor = (e: Entry) => <Icon name={iconOf(e)} className="size-4 shrink-0 text-muted-foreground" />

  return (
    <div className="@container/files flex h-full min-h-0 flex-col bg-background text-sm text-foreground">
      <header className="flex flex-wrap items-center gap-1.5 border-b border-border px-2.5 py-2">
        <Select
          items={(places ?? []).map((p) => ({ value: p.id, label: p.name }))}
          value={place || null}
          onValueChange={(v) => {
            if (typeof v === "string" && v !== place) go(v, "")
          }}
        >
          <SelectTrigger size="sm" className="max-w-36 min-w-24" aria-label="Place">
            <SelectValue placeholder="Place" />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false} align="start">
            {(places ?? []).map((p) => (
              <SelectItem key={p.id} value={p.id} title={p.path}>
                {p.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <IconButton label="Add a place" onClick={() => setAdding((a) => !a)}>
          <Icon name="plus" />
        </IconButton>
        <IconButton label="Remove this place (its files stay)" disabled={!place} onClick={() => void removePlace()}>
          <Icon name="minus" />
        </IconButton>
        <Breadcrumb
          className="order-last min-w-0 basis-full px-1 @2xl/files:order-none @2xl/files:flex-1 @2xl/files:basis-0"
          aria-label="Folder"
        >
          <BreadcrumbList>
            <BreadcrumbItem>
              {crumbs.length === 0 ? (
                <BreadcrumbPage>{placeName ?? place}</BreadcrumbPage>
              ) : (
                <BreadcrumbLink
                  href="#"
                  onClick={(e) => {
                    e.preventDefault()
                    go(place, "")
                  }}
                >
                  {placeName ?? place}
                </BreadcrumbLink>
              )}
            </BreadcrumbItem>
            {crumbs.map((seg, i) => {
              const to = crumbs.slice(0, i + 1).join("/")
              return [
                <BreadcrumbSeparator key={`${to}/`} />,
                <BreadcrumbItem key={to}>
                  {i === crumbs.length - 1 ? (
                    <BreadcrumbPage>{seg}</BreadcrumbPage>
                  ) : (
                    <BreadcrumbLink
                      href="#"
                      onClick={(e) => {
                        e.preventDefault()
                        go(place, to)
                      }}
                    >
                      {seg}
                    </BreadcrumbLink>
                  )}
                </BreadcrumbItem>,
              ]
            })}
          </BreadcrumbList>
        </Breadcrumb>
        <Input
          className="ml-auto min-w-0 flex-1 @2xl/files:ml-0 @2xl/files:w-40 @2xl/files:flex-none"
          placeholder="Filter…"
          value={filter}
          aria-label="Filter"
          onChange={(e) => setFilter(e.target.value)}
        />
        <Button
          variant="outline"
          aria-label="New folder"
          title="New folder"
          disabled={!place}
          onClick={() => setNaming({ kind: "folder" })}
        >
          <Icon name="folder-plus" />
          <span className="hidden @lg/files:inline">New folder</span>
        </Button>
      </header>
      {adding && <AddPlace onAdd={addPlace} onCancel={() => setAdding(false)} />}
      {error && <p className="px-3 pt-2 text-destructive">{error}</p>}
      {note && <p className="px-3 pt-2 text-muted-foreground">{note}</p>}
      {places?.length === 0 && (
        <p className="px-3 pt-2 text-muted-foreground">There are no places yet. Add one with the + button.</p>
      )}
      {/* A narrow window puts the preview under the list instead of squeezing both. */}
      <div className="flex min-h-0 flex-1 flex-col @2xl/files:flex-row">
        <div className="@container/list flex min-h-0 min-w-0 flex-1 basis-0 flex-col overflow-auto">
          <Table className="table-fixed">
            <TableHeader>
              <TableRow>
                <TableHead className="pl-3">Name</TableHead>
                <TableHead className="hidden w-20 text-right @sm/list:table-cell">Size</TableHead>
                <TableHead className="hidden w-44 @xl/list:table-cell">Modified</TableHead>
                <TableHead className="w-18">
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {naming?.kind === "folder" && (
                <TableRow>
                  <TableCell className="py-1 pl-3">
                    <div className="flex items-center gap-2">
                      <Icon name="folder" className="size-4 shrink-0 text-muted-foreground" />
                      <NameBox initial="" onDone={makeFolder} onCancel={() => setNaming(null)} />
                    </div>
                  </TableCell>
                  <TableCell className={sizeCell} />
                  <TableCell className={timeCell} />
                  <TableCell />
                </TableRow>
              )}
              {shown.map((e) => {
                const renaming = naming?.kind === "rename" && naming.entry.path === e.path
                return (
                  <TableRow
                    key={e.path}
                    data-state={e.path === selection ? "selected" : undefined}
                    data-locked={e.locked === true}
                  >
                    {renaming ? (
                      <TableCell className="py-1 pl-3">
                        <div className="flex items-center gap-2">
                          {iconFor(e)}
                          <NameBox initial={e.name} onDone={(n) => rename(e, n)} onCancel={() => setNaming(null)} />
                        </div>
                      </TableCell>
                    ) : (
                      <TableCell
                        className="py-1 pl-1"
                        title={e.locked ? "Files does not open things that look like credentials." : e.name}
                      >
                        <Button
                          variant="ghost"
                          size="sm"
                          className="max-w-full justify-start"
                          disabled={e.locked}
                          onClick={() => (e.dir ? go(place, e.path) : setSelected(e.path))}
                        >
                          {iconFor(e)}
                          <span className="truncate">{e.name}</span>
                          {e.symlink && <Icon name="link" className="size-3 text-muted-foreground" />}
                        </Button>
                      </TableCell>
                    )}
                    <TableCell className={sizeCell}>{e.dir ? "" : formatSize(e.size)}</TableCell>
                    <TableCell className={timeCell}>{formatTime(e.modified)}</TableCell>
                    <TableCell className="py-1">
                      {!e.locked && !renaming && (
                        <div className="invisible flex justify-end [tr:focus-within_&]:visible [tr:hover_&]:visible">
                          <IconButton label="Rename" onClick={() => setNaming({ kind: "rename", entry: e })}>
                            <Icon name="pencil" />
                          </IconButton>
                          <IconButton label="Delete" onClick={() => void remove(e)}>
                            <Icon name="trash" />
                          </IconButton>
                        </div>
                      )}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
          {shown.length === 0 && !error && naming?.kind !== "folder" && (
            <Empty>
              <EmptyHeader>
                <EmptyDescription>{needle ? "Nothing here matches." : "This folder is empty."}</EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
        </div>
        {current && (
          <section
            className="flex min-h-0 min-w-0 flex-1 basis-0 flex-col border-t border-border @2xl/files:border-t-0 @2xl/files:border-l"
            aria-label={`Preview of ${current.name}`}
          >
            <div className="flex items-center gap-1 border-b border-border py-1.5 pr-2 pl-3">
              <span className="min-w-0 flex-1 truncate font-medium" title={current.path}>
                {current.name}
              </span>
              {NOTE.test(current.name) && (
                <Button
                  variant="outline"
                  size="sm"
                  aria-label="Open in Notes"
                  title="Open in Notes"
                  onClick={() => openInNotes(current)}
                >
                  <Icon name="notebook-pen" />
                  <span className="hidden @sm/files:inline">Open in Notes</span>
                </Button>
              )}
              <IconButton
                label="Download"
                nativeButton={false}
                render={<a href={api.url(`raw?${at(place, current.path)}&download=1`)} />}
              >
                <Icon name="download" />
              </IconButton>
              <IconButton label="Close the preview" onClick={() => setSelected("")}>
                <Icon name="x" />
              </IconButton>
            </div>
            <Preview api={api} place={place} entry={current} limitKb={limitKb} />
          </section>
        )}
      </div>
      <footer className="truncate border-t border-border px-3 py-1.5 text-xs text-muted-foreground">
        {needle ? `${shown.length} of ${entries.length}` : entries.length} {entries.length === 1 ? "item" : "items"}
        {showHidden ? " · hidden files shown" : " · hidden files not shown"}
        {current ? ` · ${current.name}, ${formatSize(current.size)}` : ""}
      </footer>
    </div>
  )
}

export default { views: { "files/main": Files } } satisfies ModuleWeb
