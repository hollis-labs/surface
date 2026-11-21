/**
 * Browser: a page in a window. It is shown in a frame when the site allows that, else in
 * reader mode (the page's words and links as the server read them). History lives in the
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
  const [tick, setTick] = useState(0)
  const input = useRef<HTMLInputElement>(null)
  const addressAnchor = useAnchor(win, "address", "The address bar")
  const pageAnchor = useAnchor(win, "page", "The page")

  useEffect(() => setAddress(url), [url])
  useEffect(() => {
    if (win.args.focus_address || !url) input.current?.focus()
  }, [win.args.focus_address, url])

  const showReader = !page?.search && (reader || page?.public_only === true || (page !== null && !page.framable))

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
    async (to: string) => {
      if (!to.trim()) return
      const r = await api.get<{ url: string }>(`resolve?q=${encodeURIComponent(to)}`).catch((e: Error) => {
        setErr(e.message)
        return null
      })
      if (!r) return
      const hist = [...history.slice(0, at + 1), r.url].slice(-100)
      void desk.setArgs(win.id, { url: r.url, history: hist, at: hist.length - 1, public_only: false })
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
  const unframable = page !== null && (!page.framable || page.public_only === true)
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
        page?.public_only ? (
          <span>{alt || "Image"}</span>
        ) : (
          <img src={src} alt={alt ?? ""} className="max-w-full" loading="lazy" referrerPolicy="no-referrer" />
        ),
    }),
    [go, page?.public_only],
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
        <Tip
          label={
            page?.public_only
              ? "Read-only page opened by the agent"
              : page?.search
                ? "Search results"
                : unframable
                  ? "This site cannot be shown in a frame: reader mode"
                  : "Reader mode"
          }
        >
          <Toggle
            size="sm"
            aria-label="Reader mode"
            pressed={showReader}
            disabled={unframable || page?.search}
            onPressedChange={setReader}
          >
            <BookOpenText />
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
        {url && page?.url === url && !win.args.public_only && !page.search && !showReader && (
          <iframe
            key={`${url}#${tick}`}
            className="browser-frame block size-full border-0"
            src={url}
            title={page?.title ?? url}
            sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-popups-to-escape-sandbox"
            referrerPolicy="no-referrer"
          />
        )}
        {loading && (
          <p role="status" className="p-4 text-sm text-muted-foreground">
            Loading…
          </p>
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
        {url && showReader && page && (
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
