/**
 * Frag's windows: the capture box (text, tags, a preview, commit) and the outbox (where each
 * capture is on its way to Fragments Engine; retry or discard one).
 *
 * The draft is the server's: every edit is kept there, so closing the window or reloading the
 * page loses nothing. What is sent is exactly the text typed; the preview is only a view.
 */
import {
  ApiError,
  Icon,
  type ModuleWeb,
  setSetting,
  useEvent,
  useSetting,
  type ViewProps,
} from "@hollis-labs/surface-page/sdk"
import { type KeyboardEvent, useCallback, useEffect, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Item, ItemActions, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item"
import { Kbd } from "@/components/ui/kbd"
import { Markdown } from "@/components/ui/markdown"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

interface Draft {
  content: string
  tags: string
  draft_at?: string
  capture_id?: string
}

type State = "queued" | "needs_attention" | "saved" | "discarded"

interface Capture {
  sequence: number
  capture_id: string
  state: State
  code: string
  content: string
  tags: string[]
  created_at: string
  updated_at: string
  attempts: number
  next_attempt?: number
  may_have_sent: boolean
  delivering: boolean
  fragment_id?: string
  fragment_url?: string
}

interface Status {
  delivery: boolean
  ready: boolean
  reason?: string
  fe_url?: string
  counts: Record<State, number>
}

const words = (code: string) => code.replaceAll("_", " ")

/** What the status line says about where a capture is. */
function described(c: Capture, st: Status | null): { tone: "ok" | "busy" | "error"; text: string } {
  if (c.state === "saved")
    return { tone: "ok", text: `saved to Fragments Engine · ${c.fragment_id?.slice(0, 12) ?? ""}` }
  if (c.state === "needs_attention")
    return { tone: "error", text: `needs attention · ${words(c.code)} · see the outbox` }
  if (c.state === "discarded") return { tone: "error", text: `discarded · ${words(c.code)}` }
  if (c.delivering) return { tone: "busy", text: "queued · sending to Fragments Engine…" }
  if (st && !st.ready) return { tone: "ok", text: `queued here · ${st.reason ?? "delivery is off"}` }
  return { tone: "ok", text: "queued here" }
}

const TONE: Record<string, string> = {
  ok: "text-muted-foreground",
  busy: "text-muted-foreground",
  error: "text-destructive",
}

function CaptureBox({ win, desk, api }: ViewProps) {
  const [draft, setDraft] = useState<Draft>({ content: "", tags: "" })
  const [loaded, setLoaded] = useState(false)
  const [status, setStatus] = useState<Status | null>(null)
  const [line, setLine] = useState<{ tone: string; text: string }>({ tone: "ok", text: "" })
  const [followed, setFollowed] = useState<number | null>(null)
  const preview = useSetting<boolean>("frag.preview") ?? false
  const box = useRef<HTMLTextAreaElement>(null)
  const timer = useRef<number | undefined>(undefined)
  const saving = useRef(false)

  const loadDraft = useCallback(() => {
    void api
      .get<Draft>("draft")
      .then((d) => {
        setDraft({ content: d.content ?? "", tags: d.tags ?? "" })
        setLoaded(true)
      })
      .catch((e: Error) => setLine({ tone: "error", text: e.message }))
  }, [api])
  const loadStatus = useCallback(() => {
    void api
      .get<Status>("status")
      .then(setStatus)
      .catch(() => setStatus(null))
  }, [api])
  useEffect(loadDraft, [loadDraft])
  useEffect(loadStatus, [loadStatus])
  useEvent("frag.status", loadStatus)
  // Committed elsewhere (another window, the agent): show what is left.
  useEvent<{ committed?: number }>("frag.draft", (ev) => {
    if (ev.data?.committed && timer.current === undefined) loadDraft()
  })
  // Follow the last capture until it is delivered.
  useEvent<{ sequence?: number }>("frag.outbox", (ev) => {
    if (followed === null || ev.data?.sequence !== followed) return
    void api.get<Capture>(`outbox/${followed}`).then((c) => setLine(described(c, status)))
  })

  // biome-ignore lint/correctness/useExhaustiveDependencies: opening with compose again refocuses the box
  useEffect(() => {
    if (win.focused && !preview) box.current?.focus()
  }, [win.focused, win.args.compose, preview])

  useEffect(() => {
    desk.report(win.id, { kind: "data", ref: "frag:draft" })
  }, [desk, win.id])

  // Every edit is kept on the server, a moment after the typing stops.
  const edit = (patch: Partial<Draft>) => {
    const next = { ...draft, ...patch }
    setDraft(next)
    if (line.tone === "error") setLine({ tone: "ok", text: "" })
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => {
      timer.current = undefined
      void api
        .put("draft", { content: next.content, tags: next.tags })
        .catch((e: Error) => setLine({ tone: "error", text: e.message }))
    }, 300)
  }

  const commit = async () => {
    if (saving.current) return
    if (!draft.content.trim()) {
      setLine({ tone: "error", text: "there is nothing to capture" })
      return
    }
    saving.current = true
    setLine({ tone: "busy", text: "queueing…" })
    try {
      window.clearTimeout(timer.current)
      timer.current = undefined
      await api.put("draft", { content: draft.content, tags: draft.tags })
      const c = await api.post<Capture>("draft/commit")
      setDraft({ content: "", tags: "" })
      setFollowed(c.sequence)
      setLine(described(c, status))
    } catch (e) {
      const msg = e instanceof ApiError || e instanceof Error ? e.message : "it could not be queued"
      setLine({ tone: "error", text: `${msg} · the draft is kept` })
    } finally {
      saving.current = false
    }
  }

  const togglePreview = () => void setSetting("frag.preview", !preview)

  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault()
      void commit()
    } else if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.code === "KeyP") {
      e.preventDefault()
      togglePreview()
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col" onKeyDown={onKey}>
      <div className="min-h-0 flex-1 overflow-auto p-3">
        {preview ? (
          draft.content.trim() ? (
            <Markdown>{draft.content}</Markdown>
          ) : (
            <p className="text-muted-foreground text-sm">Nothing written yet.</p>
          )
        ) : (
          <Textarea
            ref={box}
            className="h-full min-h-32 resize-none font-mono"
            value={draft.content}
            disabled={!loaded}
            aria-label="Fragment"
            placeholder="Write it down…"
            onChange={(e) => edit({ content: e.target.value })}
          />
        )}
      </div>
      <div className="flex items-center gap-2 border-t px-3 py-2">
        <Icon name="tag" size={14} className="shrink-0 text-muted-foreground" />
        <Input
          className="flex-1"
          value={draft.tags}
          disabled={!loaded}
          spellCheck={false}
          aria-label="Tags, separated by commas"
          placeholder="idea, later"
          onChange={(e) => edit({ tags: e.target.value })}
        />
        <Button disabled={!draft.content.trim()} onClick={() => void commit()}>
          Commit
        </Button>
      </div>
      <footer className="flex items-center gap-3 border-t px-3 py-1.5 text-xs">
        <span className={`min-w-0 flex-1 truncate ${TONE[line.tone] ?? ""}`} role="status">
          {line.text || (status && !status.ready ? `delivery: ${status.reason}` : "")}
        </span>
        <span className="flex items-center gap-1.5 text-muted-foreground">
          <Switch size="sm" checked={preview} onCheckedChange={togglePreview} aria-label="Preview" />
          preview
        </span>
        <Button size="xs" variant="ghost" onClick={() => void desk.open("frag.outbox")}>
          Outbox
        </Button>
        <span className="text-muted-foreground">
          <Kbd>⌘↵</Kbd> commit
        </span>
      </footer>
    </div>
  )
}

const STATE_BADGE: Record<State, "default" | "secondary" | "destructive" | "outline"> = {
  queued: "secondary",
  needs_attention: "destructive",
  saved: "default",
  discarded: "outline",
}

type Filter = "all" | State

function Outbox({ win, desk, api }: ViewProps) {
  const [items, setItems] = useState<Capture[]>([])
  const [status, setStatus] = useState<Status | null>(null)
  const [filter, setFilter] = useState<Filter>("all")
  const [error, setError] = useState("")
  const [check, setCheck] = useState("")

  const load = useCallback(() => {
    const q = filter === "all" ? "" : `?state=${filter}`
    void api
      .get<Capture[]>(`outbox${q}`)
      .then((l) => {
        setItems(l)
        setError("")
      })
      .catch((e: Error) => setError(e.message))
    void api
      .get<Status>("status")
      .then(setStatus)
      .catch(() => setStatus(null))
  }, [api, filter])
  useEffect(load, [load])
  useEvent("frag.outbox", load)
  useEvent("frag.status", load)

  useEffect(() => {
    desk.report(win.id, { kind: "data", ref: "frag:outbox", filter })
  }, [desk, win.id, filter])

  const act = (p: Promise<unknown>) => void p.then(load).catch((e: Error) => setError(e.message))
  const discard = async (c: Capture) => {
    const ok = await desk.confirm({
      title: c.may_have_sent ? "Stop delivering this capture?" : "Discard this capture?",
      message: c.may_have_sent
        ? "Fragments Engine may already hold a copy; it is not deleted there. A tombstone is kept here so the capture is not sent again."
        : "It was never sent. Its text is cleared here.",
      confirmLabel: c.may_have_sent ? "Stop delivery" : "Discard",
      danger: true,
    })
    if (ok) act(api.del(`outbox/${c.sequence}`))
  }
  const runCheck = () => {
    setCheck("checking…")
    void api
      .post<{ ok: boolean; reason?: string; fe_url?: string }>("check")
      .then((r) => setCheck(r.ok ? `${r.fe_url} accepts the key` : `not accepted: ${r.reason}`))
      .catch((e: Error) => setCheck(e.message))
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
        <ToggleGroup
          size="sm"
          spacing={0}
          value={[filter]}
          onValueChange={(v: string[]) => v[0] && setFilter(v[0] as Filter)}
        >
          <ToggleGroupItem value="all">All</ToggleGroupItem>
          <ToggleGroupItem value="queued">Queued</ToggleGroupItem>
          <ToggleGroupItem value="needs_attention">Attention</ToggleGroupItem>
          <ToggleGroupItem value="saved">Saved</ToggleGroupItem>
          <ToggleGroupItem value="discarded">Discarded</ToggleGroupItem>
        </ToggleGroup>
        <div className="ml-auto flex items-center gap-1">
          <Button
            size="sm"
            variant="ghost"
            title="Read Fragments Engine with the key; sends nothing"
            onClick={runCheck}
          >
            Check connection
          </Button>
          <Button size="sm" variant="outline" onClick={() => void desk.run("frag.capture")}>
            <Icon name="feather" /> Capture
          </Button>
        </div>
      </div>
      <div className="flex flex-wrap gap-x-3 border-b px-3 py-1.5 text-muted-foreground text-xs">
        <span>
          {status ? (status.ready ? `delivering to ${status.fe_url}` : `not delivering: ${status.reason}`) : ""}
        </span>
        {status && (
          <span>
            {status.counts.queued} queued · {status.counts.needs_attention} need attention · {status.counts.saved} saved
          </span>
        )}
        {check && <span>{check}</span>}
      </div>
      {error && <p className="px-3 py-1.5 text-destructive text-xs">{error}</p>}
      <div className="min-h-0 flex-1 overflow-auto p-1.5">
        {items.length === 0 && (
          <Empty>
            <EmptyHeader>
              <EmptyDescription>No captures{filter === "all" ? " yet" : " here"}.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        {items.map((c) => {
          const waiting = (c.state === "queued" || c.state === "needs_attention") && !c.delivering
          return (
            <Item key={c.sequence} size="sm">
              <ItemContent>
                <ItemTitle>
                  <Badge variant={STATE_BADGE[c.state]}>{words(c.state)}</Badge>
                  <span className="truncate">{c.content ? c.content.split("\n")[0] : <em>(cleared)</em>}</span>
                </ItemTitle>
                <ItemDescription>
                  #{c.sequence} · {words(c.code)}
                  {c.attempts > 0 && ` · ${c.attempts} ${c.attempts === 1 ? "attempt" : "attempts"}`}
                  {c.tags.length > 0 && ` · ${c.tags.join(", ")}`}
                  {c.fragment_url && (
                    <>
                      {" · "}
                      <a href={c.fragment_url} target="_blank" rel="noreferrer noopener" className="underline">
                        {c.fragment_id?.slice(0, 12)}
                      </a>
                    </>
                  )}
                </ItemDescription>
              </ItemContent>
              <ItemActions>
                {waiting && (
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    title="Retry now"
                    onClick={() => act(api.post(`outbox/${c.sequence}/retry`))}
                  >
                    <Icon name="rotate-cw" />
                  </Button>
                )}
                {waiting && (
                  <Button variant="ghost" size="icon-xs" title="Discard" onClick={() => void discard(c)}>
                    <Icon name="trash-2" />
                  </Button>
                )}
              </ItemActions>
            </Item>
          )
        })}
      </div>
    </div>
  )
}

export default { views: { "frag/capture": CaptureBox, "frag/outbox": Outbox } } satisfies ModuleWeb
