/**
 * Files' window: a browser over the owner's places. The place and folder live in the
 * window's args, so they survive a reload and a saved layout, and the agent can take the
 * window somewhere by setting them.
 */
import { Icon, type ModuleAPI, type ModuleWeb, useEvent, useSetting, type ViewProps } from "@surface/sdk"
import { type FormEvent, useCallback, useEffect, useRef, useState } from "react"
import "./files.css"

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
    <form className="files-namebox" onSubmit={submit}>
      <input
        ref={box}
        className="sf-input sf-grow"
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
    <form className="files-add sf-row" onSubmit={submit}>
      <input
        className="sf-input"
        placeholder="Name"
        value={name}
        aria-label="Place name"
        onChange={(e) => setName(e.target.value)}
      />
      <input
        className="sf-input sf-grow"
        placeholder="Folder, like ~/Documents"
        value={path}
        aria-label="Folder path"
        onChange={(e) => setPath(e.target.value)}
      />
      <button type="submit" className="sf-btn" data-kind="primary" disabled={!path.trim()}>
        Add place
      </button>
      <button type="button" className="sf-btn" onClick={onCancel}>
        Cancel
      </button>
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
      <div className="files-preview-body files-preview-image">
        <img src={api.url(`raw?${at(place, entry.path)}&v=${encodeURIComponent(stamp)}`)} alt={entry.name} />
      </div>
    )
  }
  if (error) {
    return (
      <div className="files-preview-body sf-pane">
        <p className="sf-dim">No text preview: {error}</p>
      </div>
    )
  }
  if (!part) return <div className="files-preview-body sf-pane sf-dim">Reading…</div>
  return (
    <div className="files-preview-body">
      {part.truncated && (
        <p className="sf-dim files-truncated">
          Showing the first {formatSize(Math.min(limitKb * 1024, part.size))} of {formatSize(part.size)}. The preview
          limit is a Files setting.
        </p>
      )}
      {part.text ? (
        <pre className="files-text">{part.text}</pre>
      ) : (
        <p className="sf-dim files-truncated">This file is empty.</p>
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

  return (
    <div className="files">
      <header className="files-bar">
        <select
          className="sf-input files-place"
          value={place}
          aria-label="Place"
          onChange={(e) => go(e.target.value, "")}
        >
          {(places ?? []).map((p) => (
            <option key={p.id} value={p.id} title={p.path}>
              {p.name}
            </option>
          ))}
        </select>
        <button type="button" className="files-icon" title="Add a place" onClick={() => setAdding((a) => !a)}>
          <Icon name="plus" size={15} />
        </button>
        <button
          type="button"
          className="files-icon"
          title="Remove this place (its files stay)"
          disabled={!place}
          onClick={() => void removePlace()}
        >
          <Icon name="minus" size={15} />
        </button>
        <nav className="files-crumbs sf-grow" aria-label="Folder">
          <button type="button" className="files-crumb" onClick={() => go(place, "")}>
            {placeName ?? place}
          </button>
          {crumbs.map((seg, i) => {
            const to = crumbs.slice(0, i + 1).join("/")
            return (
              <span key={to} className="files-crumb-step">
                <Icon name="chevron-right" size={12} />
                <button type="button" className="files-crumb" onClick={() => go(place, to)}>
                  {seg}
                </button>
              </span>
            )
          })}
        </nav>
        <input
          className="sf-input files-filter"
          placeholder="Filter…"
          value={filter}
          aria-label="Filter"
          onChange={(e) => setFilter(e.target.value)}
        />
        <button type="button" className="sf-btn" disabled={!place} onClick={() => setNaming({ kind: "folder" })}>
          <Icon name="folder-plus" size={14} />
          New folder
        </button>
      </header>
      {adding && <AddPlace onAdd={addPlace} onCancel={() => setAdding(false)} />}
      {error && <p className="sf-error files-msg">{error}</p>}
      {note && <p className="sf-dim files-msg">{note}</p>}
      {places?.length === 0 && <p className="sf-dim files-msg">There are no places yet. Add one with the + button.</p>}
      <div className="files-main">
        <ul className="files-list">
          {naming?.kind === "folder" && (
            <li className="files-row">
              <span className="files-name">
                <Icon name="folder" size={15} />
                <NameBox initial="" onDone={makeFolder} onCancel={() => setNaming(null)} />
              </span>
            </li>
          )}
          {shown.length === 0 && !error && naming?.kind !== "folder" && (
            <li className="sf-dim files-none">{needle ? "Nothing here matches." : "This folder is empty."}</li>
          )}
          {shown.map((e) => (
            <li key={e.path} className="files-row" data-selected={e.path === selection} data-locked={e.locked === true}>
              {naming?.kind === "rename" && naming.entry.path === e.path ? (
                <span className="files-name">
                  <Icon name={iconOf(e)} size={15} />
                  <NameBox initial={e.name} onDone={(n) => rename(e, n)} onCancel={() => setNaming(null)} />
                </span>
              ) : (
                <button
                  type="button"
                  className="files-name"
                  disabled={e.locked}
                  title={e.locked ? "Files does not open things that look like credentials." : e.name}
                  onClick={() => (e.dir ? go(place, e.path) : setSelected(e.path))}
                >
                  <Icon name={iconOf(e)} size={15} />
                  <span className="files-label">{e.name}</span>
                  {e.symlink && <Icon name="link" size={11} />}
                </button>
              )}
              <span className="files-size sf-dim">{e.dir ? "" : formatSize(e.size)}</span>
              <span className="files-time sf-dim">{formatTime(e.modified)}</span>
              <span className="files-acts">
                {!e.locked && (
                  <>
                    <button
                      type="button"
                      className="files-icon"
                      title="Rename"
                      onClick={() => setNaming({ kind: "rename", entry: e })}
                    >
                      <Icon name="pencil" size={13} />
                    </button>
                    <button type="button" className="files-icon" title="Delete" onClick={() => void remove(e)}>
                      <Icon name="trash" size={13} />
                    </button>
                  </>
                )}
              </span>
            </li>
          ))}
        </ul>
        {current && (
          <section className="files-preview" aria-label={`Preview of ${current.name}`}>
            <div className="files-preview-head">
              <span className="files-label sf-grow" title={current.path}>
                {current.name}
              </span>
              {NOTE.test(current.name) && (
                <button type="button" className="sf-btn" onClick={() => openInNotes(current)}>
                  <Icon name="notebook-pen" size={14} />
                  Open in Notes
                </button>
              )}
              <a className="files-icon" title="Download" href={api.url(`raw?${at(place, current.path)}&download=1`)}>
                <Icon name="download" size={15} />
              </a>
              <button type="button" className="files-icon" title="Close the preview" onClick={() => setSelected("")}>
                <Icon name="x" size={15} />
              </button>
            </div>
            <Preview api={api} place={place} entry={current} limitKb={limitKb} />
          </section>
        )}
      </div>
      <footer className="files-foot sf-dim">
        {needle ? `${shown.length} of ${entries.length}` : entries.length} {entries.length === 1 ? "item" : "items"}
        {showHidden ? " · hidden files shown" : " · hidden files not shown"}
        {current ? ` · ${current.name}, ${formatSize(current.size)}` : ""}
      </footer>
    </div>
  )
}

export default { views: { "files/main": Files } } satisfies ModuleWeb
