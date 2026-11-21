/**
 * Browser: a page in a window. It is shown in a frame when the site allows that, else in
 * a static sandboxed snapshot, with Reader fallback. History lives in the
 * window's args, so back and forward survive a reload and a saved layout.
 */

import { type ModuleWeb, useAnchor, useSetting, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { ArrowLeft, ArrowRight, BookOpenText, ExternalLink, Globe, House, RotateCw } from "lucide-react"
import {
  type ComponentProps,
  type FormEvent,
  type ReactElement,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Markdown } from "@/components/ui/markdown"
import { Spinner } from "@/components/ui/spinner"
import { Toggle } from "@/components/ui/toggle"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import "./browser.css"

interface Snapshot {
  url: string
  final_url: string
  title: string
  html: string
  fetched: string
  links: { text: string; url: string }[]
  missing_assets: number
  dropped_css: number
}

// Reserve before fetching; the server supplies budgets from limits.go.
// Only byte reservations are shared; bodies live in the requesting window.
const snapshotReservations = new Set<symbol>()

async function readSnapshot(response: Response, signal: AbortSignal, maxBytes: number): Promise<Snapshot> {
  const stream = response.body?.getReader()
  if (!stream) throw new Error("Empty snapshot response")
  const chunks: Uint8Array[] = []
  let size = 0
  try {
    for (;;) {
      signal.throwIfAborted()
      const { value, done } = await stream.read()
      if (done) break
      size += value.byteLength
      if (size > maxBytes) throw new Error("Snapshot exceeds the window memory limit")
      chunks.push(value)
    }
  } finally {
    await stream.cancel()
  }
  const bytes = new Uint8Array(size)
  let offset = 0
  for (const chunk of chunks) {
    bytes.set(chunk, offset)
    offset += chunk.length
  }
  const data = JSON.parse(new TextDecoder().decode(bytes))
  if (!response.ok) throw new Error(data.error || "Snapshot failed")
  return data as Snapshot
}

interface Page {
  url: string
  final_url: string
  title: string
  text: string
  links?: { text: string; url: string }[]
  search?: boolean
  query?: string
  results?: { title: string; url: string; snippet?: string }[]
  public_only?: boolean
  framable: boolean
}

function hostOf(u: string) {
  try {
    return new URL(u).host.replace(/^www\./, "")
  } catch {
    return u
  }
}

/** An icon control with its name in a tooltip. */
function Tip({ label, children }: { label: string; children: ReactElement }) {
  return (
    <Tooltip>
      <TooltipTrigger render={children} />
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

function BrowserView({ win, desk, api }: ViewProps) {
  const home = useSetting<string>("browser.home") ?? "https://duckduckgo.com/"
  const readerDefault = useSetting<boolean>("browser.reader") ?? false
  const url = typeof win.args.url === "string" ? win.args.url : ""
  const history = useMemo(
    () => (Array.isArray(win.args.history) ? (win.args.history as string[]) : url ? [url] : []),
    [win.args.history, url],
  )
  const at = typeof win.args.at === "number" ? win.args.at : history.length - 1
  const [address, setAddress] = useState(url)
  const [page, setPage] = useState<Page | null>(null)
  const [err, setErr] = useState("")
  const [loading, setLoading] = useState(false)
  const [reader, setReader] = useState<boolean>(readerDefault)
  const [snapshotMode, setSnapshotMode] = useState(false)
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [snapshotError, setSnapshotError] = useState("")
  const [snapshotLoading, setSnapshotLoading] = useState(false)
  const [tick, setTick] = useState(0)
  const input = useRef<HTMLInputElement>(null)
  const addressAnchor = useAnchor(win, "address", "The address bar")
  const pageAnchor = useAnchor(win, "page", "The page")

  useEffect(() => setAddress(url), [url])
  useEffect(() => {
    if (win.args.focus_address || !url) input.current?.focus()
  }, [win.args.focus_address, url])

  const wantsSnapshot =
    !reader && !page?.search && page?.url === url && (snapshotMode || page.public_only === true || !page.framable)
  const showSnapshot = wantsSnapshot && snapshot?.url === url
  const showReader = !page?.search && (reader || (wantsSnapshot && !!snapshotError))

  // Abort requests and release both the body and its reservation on navigation/close.
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick deliberately reloads a snapshot
  useEffect(() => {
    setSnapshot(null)
    setSnapshotError("")
    setSnapshotLoading(false)
    if (!wantsSnapshot) return
    const reservation = Symbol(win.id)
    const controller = new AbortController()
    setSnapshotLoading(true)
    const load = async () => {
      const policy = await fetch(api.url("snapshot-limits"), { signal: controller.signal })
      if (!policy.ok) throw new Error("Snapshot limits unavailable")
      const limits: { max_response_bytes: number; max_windows: number } = await policy.json()
      controller.signal.throwIfAborted()
      if (
        !Number.isSafeInteger(limits.max_response_bytes) ||
        limits.max_response_bytes <= 0 ||
        !Number.isSafeInteger(limits.max_windows) ||
        limits.max_windows <= 0
      )
        throw new Error("Invalid snapshot limits")
      if (snapshotReservations.size >= limits.max_windows)
        throw new Error("Close another styled snapshot to free memory")
      snapshotReservations.add(reservation)
      const response = await fetch(api.url(`snapshot?url=${encodeURIComponent(url)}`), {
        method: "POST",
        signal: controller.signal,
        cache: "no-store",
      })
      return readSnapshot(response, controller.signal, limits.max_response_bytes)
    }
    load()
      .then((result) => {
        if (!controller.signal.aborted) setSnapshot(result)
      })
      .catch((error: Error) => {
        if (!controller.signal.aborted) {
          setSnapshotError(`${error.message}. Showing Reader.`)
          snapshotReservations.delete(reservation)
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setSnapshotLoading(false)
      })
    return () => {
      controller.abort()
      snapshotReservations.delete(reservation)
    }
  }, [wantsSnapshot, url, tick, api, win.id])

  // Read the page on the server: its title, whether it may be framed, and the reader text.
  // Only a new address or a reload (tick) reads it again.
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick forces a reload; desk is read, not watched
  useEffect(() => {
    if (!url) return
    let live = true
    setPage(null)
    setLoading(true)
    setErr("")
    api
      .post<Page>(`read?url=${encodeURIComponent(url)}&public=${win.args.public_only === true}`, {})
      .then((p) => {
        if (!live) return
        setPage(p)
        void desk.setTitle(win.id, p.title || hostOf(url))
      })
      .catch((e: Error) => live && setErr(e.message))
      .finally(() => live && setLoading(false))
    return () => {
      live = false
    }
  }, [url, tick, api, win.args.public_only])

  // desk.report replaces the window's state: report the whole of it.
  useEffect(() => {
    if (page) desk.report(win.id, { title: page.title, reader: showReader })
  }, [page, showReader, desk, win.id])

  const go = useCallback(
    async (to: string, publicOnly = false) => {
      if (!to.trim()) return
      const r = await api.get<{ url: string }>(`resolve?q=${encodeURIComponent(to)}`).catch((e: Error) => {
        setErr(e.message)
        return null
      })
      if (!r) return
      const hist = [...history.slice(0, at + 1), r.url].slice(-100)
      void desk.setArgs(win.id, { url: r.url, history: hist, at: hist.length - 1, public_only: publicOnly })
    },
    [api, desk, win.id, history, at],
  )
  const step = (d: number) => {
    const i = at + d
    if (i < 0 || i >= history.length) return
    void desk.setArgs(win.id, { url: history[i], history, at: i })
  }
  const submit = (e: FormEvent) => {
    e.preventDefault()
    void go(address)
  }
  const onReaderSelect = () => {
    const s = window.getSelection()?.toString().trim() ?? ""
    desk.report(win.id, { title: page?.title, reader: true, selection: s.slice(0, 2000) })
  }
  const readerComponents = useMemo<ComponentProps<typeof Markdown>["components"]>(
    () => ({
      a: ({ href, children }) => (
        <a
          href={href}
          onClick={(e) => {
            if (href) {
              e.preventDefault()
              void go(href)
            }
          }}
        >
          {children}
        </a>
      ),
      img: ({ src, alt }) =>
        page?.public_only || win.args.public_only === true || wantsSnapshot ? (
          <span>{alt || "Image"}</span>
        ) : (
          <img src={src} alt={alt ?? ""} className="max-w-full" loading="lazy" referrerPolicy="no-referrer" />
        ),
    }),
    [go, page?.public_only, win.args.public_only, wantsSnapshot],
  )

  return (
    <div className="@container flex h-full min-h-0 flex-col">
      <form className="flex items-center gap-0.5 border-b border-border px-1.5 py-1" onSubmit={submit}>
        <Tip label="Back">
          <Button variant="ghost" size="icon-sm" aria-label="Back" disabled={at <= 0} onClick={() => step(-1)}>
            <ArrowLeft />
          </Button>
        </Tip>
        <Tip label="Forward">
          <Button
            variant="ghost"
            size="icon-sm"
            className="hidden @lg:inline-flex"
            aria-label="Forward"
            disabled={at >= history.length - 1}
            onClick={() => step(1)}
          >
            <ArrowRight />
          </Button>
        </Tip>
        <Tip label="Reload">
          <Button variant="ghost" size="icon-sm" aria-label="Reload" onClick={() => setTick((t) => t + 1)}>
            {loading ? <Spinner /> : <RotateCw />}
          </Button>
        </Tip>
        <Tip label="Home">
          <Button
            variant="ghost"
            size="icon-sm"
            className="hidden @lg:inline-flex"
            aria-label="Home"
            onClick={() => void go(home)}
          >
            <House />
          </Button>
        </Tip>
        <Input
          ref={(el) => {
            input.current = el
            return addressAnchor(el)
          }}
          className="mx-1 min-w-0 flex-1"
          value={address}
          aria-label="Address"
          placeholder="Search or enter an address"
          onChange={(e) => setAddress(e.target.value)}
          onFocus={(e) => e.target.select()}
        />
        <Tip label="Reader mode">
          <Toggle
            size="sm"
            aria-label="Reader mode"
            pressed={showReader}
            disabled={page?.search}
            onPressedChange={setReader}
          >
            <BookOpenText />
          </Toggle>
        </Tip>
        <Tip label="Styled snapshot: scripts and sign-in disabled">
          <Toggle
            size="sm"
            aria-label="Styled snapshot"
            pressed={wantsSnapshot}
            disabled={page?.search}
            onPressedChange={(pressed) => {
              setReader(!pressed)
              setSnapshotMode(pressed)
              if (pressed) setTick((t) => t + 1)
            }}
          >
            <Globe />
          </Toggle>
        </Tip>
        {url && (
          <Tip label="Open in the system browser">
            <Button
              variant="ghost"
              size="icon-sm"
              className="hidden @lg:inline-flex"
              aria-label="Open in the system browser"
              nativeButton={false}
              render={<a href={url} target="_blank" rel="noreferrer noopener" />}
            >
              <ExternalLink />
            </Button>
          </Tip>
        )}
      </form>
      {snapshotError && (
        <p role="status" className="px-3 py-1.5 text-xs text-muted-foreground">
          {snapshotError}
        </p>
      )}
      {err && <div className="bg-destructive/10 px-3 py-1.5 text-xs text-destructive">{err}</div>}
      <div ref={pageAnchor} className="min-h-0 flex-1">
        {!url && (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Globe />
              </EmptyMedia>
              <EmptyDescription>Type an address or words to search.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        {url &&
          page?.url === url &&
          !win.args.public_only &&
          !page.search &&
          !showReader &&
          !wantsSnapshot &&
          page.framable && (
            <iframe
              key={`${url}#${tick}`}
              className="browser-frame block size-full border-0"
              src={url}
              title={page?.title ?? url}
              sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-popups-to-escape-sandbox"
              referrerPolicy="no-referrer"
            />
          )}
        {(loading || snapshotLoading) && (
          <p role="status" className="p-4 text-sm text-muted-foreground">
            Loading…
          </p>
        )}
        {showSnapshot && snapshot && (
          <section className="flex h-full min-h-0 flex-col" aria-label="Styled snapshot">
            <div className="border-b border-border px-3 py-2 text-xs text-muted-foreground">
              <p>Styled snapshot · scripts and sign-in disabled</p>
              <p className="truncate">
                {snapshot.final_url} · {snapshot.fetched}
              </p>
              <p>
                {snapshot.missing_assets} missing assets · {snapshot.dropped_css} dropped CSS rules or declarations
              </p>
              <p>Forms, video, custom fonts and JavaScript apps may be incomplete. Use Reader or the system browser.</p>
              <Collapsible>
                <CollapsibleTrigger render={<Button variant="link" size="xs" />}>
                  Links ({snapshot.links.length})
                </CollapsibleTrigger>
                <CollapsibleContent className="max-h-40 overflow-auto">
                  {snapshot.links.map((link) => (
                    <div key={link.url}>
                      <Button
                        variant="link"
                        size="xs"
                        onClick={() => {
                          setSnapshotMode(true)
                          void go(link.url, true)
                        }}
                      >
                        {link.text || link.url}
                      </Button>
                    </div>
                  ))}
                </CollapsibleContent>
              </Collapsible>
            </div>
            <iframe
              className="browser-frame block min-h-0 w-full flex-1 border-0"
              srcDoc={snapshot.html}
              sandbox=""
              referrerPolicy="no-referrer"
              title={`${snapshot.title} — styled snapshot`}
            />
          </section>
        )}
        {page?.search && (
          <section className="h-full overflow-auto p-4" aria-label="Search results">
            <h1>{page.query ? `Results for “${page.query}”` : "Search DuckDuckGo"}</h1>
            {!page.query && (
              <p className="mt-3 text-sm text-muted-foreground">Type words in the address bar to search.</p>
            )}
            {page.query && !page.results?.length && (
              <p className="mt-3 text-sm text-muted-foreground">
                No results returned. Try another search or open DuckDuckGo in the system browser.
              </p>
            )}
            <ol className="mt-4 flex flex-col gap-5">
              {page.results?.map((result) => (
                <li key={result.url}>
                  <Button
                    variant="link"
                    nativeButton={false}
                    render={<a href={result.url} />}
                    onClick={(e) => {
                      e.preventDefault()
                      void go(result.url)
                    }}
                  >
                    {result.title}
                  </Button>
                  <div className="truncate text-xs text-muted-foreground">{result.url}</div>
                  {result.snippet && <p className="mt-1 text-sm">{result.snippet}</p>}
                </li>
              ))}
            </ol>
          </section>
        )}
        {url && showReader && page?.url === url && (
          <article className="h-full overflow-auto px-7 pt-4 pb-10 @max-lg:px-3.5" onMouseUp={onReaderSelect}>
            <h1>{page.title}</h1>
            <div className="truncate text-xs text-muted-foreground">{page.final_url}</div>
            {page.text ? (
              <Markdown size="document" images components={readerComponents}>
                {page.text}
              </Markdown>
            ) : (
              <p>This page has no readable article text. Open it in the system browser to view interactive content.</p>
            )}
            {page.links && page.links.length > 0 && (
              <Collapsible className="mt-6 text-xs">
                <CollapsibleTrigger render={<Button variant="link" size="xs" />}>
                  Links ({page.links.length})
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <ul className="mt-2 flex flex-col gap-1">
                    {page.links.map((l) => (
                      <li key={l.url}>
                        <a
                          href={l.url}
                          onClick={(e) => {
                            e.preventDefault()
                            void go(l.url)
                          }}
                        >
                          {l.text}
                        </a>
                        <span className="ml-1.5 text-muted-foreground">{hostOf(l.url)}</span>
                      </li>
                    ))}
                  </ul>
                </CollapsibleContent>
              </Collapsible>
            )}
          </article>
        )}
      </div>
    </div>
  )
}

const web: ModuleWeb = { views: { "browser/main": BrowserView } }
export default web
