/**
 * Viewer's window: an artifact in the middle (from a Files place), and the marks on it. Drag a
 * box on a picture, or click for a pin, and say what about it; on a video, a recording or a page,
 * press Mark first so the drag is not taken by its own controls. A mark on a video or a recording
 * keeps the moment. Marks are the app's (viewer:<place>:<path>), so the agent sees them too.
 */
import {
  addMark,
  Icon,
  type Mark,
  type ModuleWeb,
  resolveMark,
  useMarks,
  type ViewProps,
} from "@hollis-labs/surface-page/sdk"
import { type PointerEvent, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Item, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"

const REVIEWABLE = /\.(png|jpe?g|gif|webp|avif|svg|mp4|webm|mov|m4v|mp3|wav|m4a|ogg|pdf|html?)$/i
type Media = "image" | "video" | "audio" | "page"
function mediaOf(path: string): Media {
  const ext = path.split(".").pop()?.toLowerCase() ?? ""
  if (/^(mp4|webm|mov|m4v)$/.test(ext)) return "video"
  if (/^(mp3|wav|m4a|ogg)$/.test(ext)) return "audio"
  if (/^(pdf|html?)$/.test(ext)) return "page"
  return "image"
}

/** Where a mark points: a box as fractions of the artifact (a pin has no size), and a moment. */
interface Anchor {
  x: number
  y: number
  w: number
  h: number
  t?: number
}
const clock = (t: number) => `${Math.floor(t / 60)}:${String(Math.floor(t % 60)).padStart(2, "0")}`
const ref = (place: string, path: string) => `viewer:${place}:${path}`
const raw = (place: string, path: string) =>
  `/api/m/files/raw?place=${encodeURIComponent(place)}&path=${encodeURIComponent(path)}`

interface Place {
  id: string
  name: string
}
interface Entry {
  name: string
  path: string
  dir: boolean
}

/** Pick a file from a Files place: folders to walk, the files the Viewer shows. */
function Picker({ onPick }: { onPick: (place: string, path: string) => void }) {
  const [places, setPlaces] = useState<Place[]>([])
  const [place, setPlace] = useState("")
  const [dir, setDir] = useState("")
  const [entries, setEntries] = useState<Entry[]>([])
  const [error, setError] = useState("")
  useEffect(() => {
    fetch("/api/m/files/places")
      .then((r) => r.json())
      .then((p: Place[]) => {
        setPlaces(p)
        if (p[0]) setPlace(p[0].id)
      })
      .catch(() => setError("The Files module is not in this app: the Viewer reads files through it."))
  }, [])
  useEffect(() => {
    if (!place) return
    fetch(`/api/m/files/list?place=${encodeURIComponent(place)}&path=${encodeURIComponent(dir)}`)
      .then((r) => r.json())
      .then((l: { entries?: Entry[]; error?: string }) => {
        if (l.error) throw new Error(l.error)
        setEntries((l.entries ?? []).filter((e) => e.dir || REVIEWABLE.test(e.name)))
        setError("")
      })
      .catch((e: Error) => setError(e.message))
  }, [place, dir])
  const up = dir.includes("/") ? dir.slice(0, dir.lastIndexOf("/")) : ""
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-2">
        <span className="text-sm font-semibold">Look at a file</span>
        <Select
          value={place}
          onValueChange={(v) => {
            setPlace(String(v))
            setDir("")
          }}
          items={places.map((p) => ({ value: p.id, label: p.name }))}
        >
          <SelectTrigger size="sm" aria-label="Place">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {places.map((p) => (
              <SelectItem key={p.id} value={p.id}>
                {p.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <span className="min-w-0 truncate font-mono text-xs text-muted-foreground">/{dir}</span>
        {error && <span className="text-xs text-destructive">{error}</span>}
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-2">
        <div className="flex flex-col gap-0.5">
          {dir && (
            <Item
              size="sm"
              render={<a href="#up" />}
              onClick={(e) => {
                e.preventDefault()
                setDir(up)
              }}
            >
              <ItemContent>
                <ItemTitle>
                  <Icon name="corner-left-up" /> ..
                </ItemTitle>
              </ItemContent>
            </Item>
          )}
          {entries.map((e) => (
            <Item
              key={e.path}
              size="sm"
              render={<a href={`#${e.path}`} />}
              onClick={(ev) => {
                ev.preventDefault()
                if (e.dir) setDir(e.path)
                else onPick(place, e.path)
              }}
            >
              <ItemContent>
                <ItemTitle>
                  <Icon name={e.dir ? "folder" : iconOf(mediaOf(e.name))} /> {e.name}
                </ItemTitle>
              </ItemContent>
            </Item>
          ))}
          {entries.length === 0 && !error && (
            <p className="p-3 text-xs text-muted-foreground">Nothing here the Viewer shows.</p>
          )}
        </div>
      </div>
    </div>
  )
}

const iconOf = (m: Media) => ({ image: "image", video: "film", audio: "audio-lines", page: "file-text" })[m]

function Viewer({ win, desk }: ViewProps) {
  const place = typeof win.args.place === "string" ? win.args.place : ""
  const path = typeof win.args.path === "string" ? win.args.path : ""
  if (!place || !path)
    return (
      <Picker
        onPick={(p, f) =>
          void desk.open("viewer", {
            key: ref(p, f),
            title: `Viewer: ${f.split("/").pop()}`,
            args: { place: p, path: f },
          })
        }
      />
    )
  return <Artifact key={ref(place, path)} place={place} path={path} win={win} desk={desk} />
}

function Artifact({ place, path, win, desk }: { place: string; path: string } & Pick<ViewProps, "win" | "desk">) {
  const media = mediaOf(path)
  const marks = useMarks(ref(place, path))
  const open = useMemo(() => marks.filter((m) => m.status === "open"), [marks])
  const [marking, setMarking] = useState(media === "image")
  const [drag, setDrag] = useState<{ x0: number; y0: number; x1: number; y1: number } | null>(null)
  const [picked, setPicked] = useState<Anchor | null>(null)
  const [text, setText] = useState("")
  const [showList, setShowList] = useState(false)
  const [active, setActive] = useState("")
  const [now, setNow] = useState(0)
  const [error, setError] = useState("")
  const stage = useRef<HTMLDivElement>(null)
  const player = useRef<HTMLVideoElement & HTMLAudioElement>(null)

  useEffect(() => desk.report(win.id, { kind: "data", ref: ref(place, path) }), [desk, win.id, place, path])

  const frac = (e: PointerEvent) => {
    const r = stage.current?.getBoundingClientRect()
    if (!r) return { x: 0, y: 0 }
    return {
      x: Math.min(1, Math.max(0, (e.clientX - r.left) / r.width)),
      y: Math.min(1, Math.max(0, (e.clientY - r.top) / r.height)),
    }
  }
  const timed = media === "video" || media === "audio"
  const down = (e: PointerEvent) => {
    if (!marking) return
    const p = frac(e)
    setDrag({ x0: p.x, y0: p.y, x1: p.x, y1: p.y })
    ;(e.target as HTMLElement).setPointerCapture?.(e.pointerId)
  }
  const move = (e: PointerEvent) => {
    if (!drag) return
    const p = frac(e)
    setDrag({ ...drag, x1: p.x, y1: p.y })
  }
  const up = () => {
    if (!drag) return
    const box = {
      x: Math.min(drag.x0, drag.x1),
      y: Math.min(drag.y0, drag.y1),
      w: Math.abs(drag.x1 - drag.x0),
      h: Math.abs(drag.y1 - drag.y0),
    }
    const pin = box.w < 0.01 && box.h < 0.01
    setPicked({
      ...(pin ? { x: drag.x0, y: drag.y0, w: 0, h: 0 } : box),
      ...(timed ? { t: player.current?.currentTime ?? 0 } : {}),
    })
    setDrag(null)
    player.current?.pause()
  }
  const save = useCallback(() => {
    if (!picked || !text.trim()) return
    addMark({ ref: ref(place, path), body: text.trim(), anchor: picked })
      .then(() => {
        setPicked(null)
        setText("")
        setError("")
      })
      .catch((e: Error) => setError(e.message))
  }, [picked, text, place, path])
  const goTo = (m: Mark) => {
    setActive(m.id)
    const a = m.anchor as Anchor | undefined
    if (a?.t !== undefined && player.current) {
      player.current.currentTime = a.t
      player.current.pause()
    }
  }
  const src = raw(place, path)
  // On a video or a recording, a mark shows around its moment (two seconds either side).
  const shown = open.filter((m) => {
    const a = m.anchor as Anchor | undefined
    return !timed || a?.t === undefined || Math.abs(a.t - now) <= 2 || m.id === active
  })

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-2">
        <Icon name={iconOf(media)} />
        <span className="min-w-0 truncate text-sm font-semibold" title={`${place}:${path}`}>
          {path.split("/").pop()}
        </span>
        <span className="min-w-0 truncate font-mono text-xs text-muted-foreground">{path}</span>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {media !== "image" && (
            <Button
              size="sm"
              variant={marking ? "secondary" : "outline"}
              aria-pressed={marking}
              onClick={() => setMarking(!marking)}
            >
              <Icon name="crosshair" />
              {marking ? "Marking" : "Mark"}
            </Button>
          )}
          <Button
            size="sm"
            variant={showList ? "secondary" : "ghost"}
            aria-pressed={showList}
            onClick={() => setShowList(!showList)}
          >
            <Icon name="map-pin" />
            Marks
            {open.length > 0 && <Badge variant="secondary">{open.length}</Badge>}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            title="Open the folder in Files"
            onClick={() =>
              void desk.open("files", {
                args: { place, path: path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "" },
              })
            }
          >
            <Icon name="folder-open" />
          </Button>
        </div>
      </div>
      {error && <p className="border-b px-3 py-1 text-xs text-destructive">{error}</p>}
      <div className="flex min-h-0 flex-1">
        <div className="relative flex min-w-0 flex-1 items-center justify-center overflow-auto bg-desk p-4">
          <div
            ref={stage}
            className={media === "page" ? "relative size-full" : "relative inline-block max-h-full max-w-full"}
          >
            {media === "image" && (
              <img
                src={src}
                alt={path}
                className="block max-h-[calc(100vh-12rem)] max-w-full select-none"
                draggable={false}
              />
            )}
            {media === "video" && (
              // biome-ignore lint/a11y/useMediaCaption: the owner's own recordings; captions are theirs to add
              <video
                ref={player}
                src={src}
                controls
                className="block max-h-[calc(100vh-12rem)] max-w-full"
                onTimeUpdate={(e) => setNow(e.currentTarget.currentTime)}
              />
            )}
            {media === "audio" && (
              // biome-ignore lint/a11y/useMediaCaption: the owner's own recordings
              <audio
                ref={player}
                src={src}
                controls
                className="block w-[min(40rem,80vw)]"
                onTimeUpdate={(e) => setNow(e.currentTarget.currentTime)}
              />
            )}
            {media === "page" && (
              <iframe src={src} title={path} sandbox="" className="size-full rounded-md border-0 bg-white" />
            )}
            {/* The marks, and the sheet a drag is made on. */}
            <div
              className={`absolute inset-0 ${marking ? "cursor-crosshair" : "pointer-events-none"}`}
              onPointerDown={down}
              onPointerMove={move}
              onPointerUp={up}
            >
              {shown.map((m, i) => {
                const a = m.anchor as Anchor | undefined
                if (!a) return null
                const pin = a.w === 0 && a.h === 0
                return (
                  <a
                    key={m.id}
                    href={`#${m.id}`}
                    title={m.body}
                    onPointerDown={(e) => e.stopPropagation()}
                    onClick={(e) => {
                      e.preventDefault()
                      goTo(m)
                    }}
                    className={`pointer-events-auto absolute flex items-start justify-start ${pin ? "size-0" : "rounded-sm border-2"} ${m.id === active ? "border-primary bg-primary/10" : "border-warning/80"}`}
                    style={{
                      left: `${a.x * 100}%`,
                      top: `${a.y * 100}%`,
                      width: `${a.w * 100}%`,
                      height: `${a.h * 100}%`,
                    }}
                  >
                    <span className="-translate-x-1/2 -translate-y-1/2 rounded-full bg-primary px-1.5 font-mono text-[10px] text-primary-foreground">
                      {open.indexOf(m) + 1 || i + 1}
                    </span>
                  </a>
                )
              })}
              {drag && (
                <div
                  className="absolute rounded-sm border-2 border-dashed border-primary bg-primary/10"
                  style={{
                    left: `${Math.min(drag.x0, drag.x1) * 100}%`,
                    top: `${Math.min(drag.y0, drag.y1) * 100}%`,
                    width: `${Math.abs(drag.x1 - drag.x0) * 100}%`,
                    height: `${Math.abs(drag.y1 - drag.y0) * 100}%`,
                  }}
                />
              )}
              {picked && (
                <div
                  className={`absolute border-2 border-primary ${picked.w === 0 ? "size-3 -translate-x-1/2 -translate-y-1/2 rounded-full bg-primary" : "rounded-sm bg-primary/10"}`}
                  style={{
                    left: `${picked.x * 100}%`,
                    top: `${picked.y * 100}%`,
                    width: picked.w ? `${picked.w * 100}%` : undefined,
                    height: picked.h ? `${picked.h * 100}%` : undefined,
                  }}
                />
              )}
            </div>
          </div>
        </div>
        {showList && (
          <div className="flex w-72 shrink-0 flex-col gap-1 overflow-auto border-l p-2">
            {open.length === 0 && (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>No marks yet</EmptyTitle>
                  <EmptyDescription>
                    {media === "image" ? "Drag a box on it, or click for a pin." : "Press Mark, then drag a box."}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            )}
            {open.map((m, i) => {
              const a = m.anchor as Anchor | undefined
              return (
                <Item
                  key={m.id}
                  size="sm"
                  variant={m.id === active ? "muted" : "outline"}
                  render={<a href={`#${m.id}`} />}
                  onClick={(e) => {
                    e.preventDefault()
                    goTo(m)
                  }}
                >
                  <ItemContent>
                    <ItemTitle>
                      <span className="font-mono text-xs text-muted-foreground">{i + 1}</span>
                      {a?.t !== undefined && <Badge variant="outline">{clock(a.t)}</Badge>}
                      {m.author === "agent" && <Badge variant="secondary">agent</Badge>}
                    </ItemTitle>
                    <ItemDescription className="line-clamp-none whitespace-pre-wrap">{m.body}</ItemDescription>
                  </ItemContent>
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    aria-label="Done with this mark"
                    title="Done"
                    onClick={(e) => {
                      e.stopPropagation()
                      e.preventDefault()
                      void resolveMark(m.id)
                    }}
                  >
                    <Icon name="check" />
                  </Button>
                </Item>
              )
            })}
          </div>
        )}
      </div>
      {picked && (
        <div className="flex shrink-0 flex-col gap-2 border-t p-2">
          <Textarea
            autoFocus
            placeholder={picked.w === 0 ? "What about this spot?" : "What about this part?"}
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) save()
              if (e.key === "Escape") setPicked(null)
            }}
          />
          <div className="flex items-center justify-end gap-2">
            {picked.t !== undefined && (
              <span className="mr-auto text-xs text-muted-foreground">at {clock(picked.t)}</span>
            )}
            <Button size="sm" variant="ghost" onClick={() => setPicked(null)}>
              Cancel
            </Button>
            <Button size="sm" disabled={!text.trim()} onClick={save}>
              Mark it
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}

export default { views: { "viewer/main": Viewer } } satisfies ModuleWeb
