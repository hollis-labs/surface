/**
 * Browser: a page in a window. It is shown in a frame when the site allows that, else in
 * reader mode (the page's words and links as the server read them). History lives in the
 * window's args, so back and forward survive a reload and a saved layout.
 */
import { Icon, type ModuleWeb, useSetting, type ViewProps } from "@surface/sdk"
import { type FormEvent, useEffect, useMemo, useRef, useState } from "react"

interface Page {
  url: string
  final_url: string
  title: string
  text: string
  links?: { text: string; url: string }[]
  framable: boolean
}

const btn = "inline-flex h-7 w-7 items-center justify-center rounded hover:bg-black/5 dark:hover:bg-white/10 disabled:opacity-30"

function hostOf(u: string) {
  try {
    return new URL(u).host.replace(/^www\./, "")
  } catch {
    return u
  }
}

function BrowserView({ win, desk, api }: ViewProps) {
  const home = useSetting<string>("browser.home") ?? "https://duckduckgo.com/"
  const readerDefault = useSetting<boolean>("browser.reader") ?? false
  const url = typeof win.args.url === "string" ? win.args.url : ""
  const history = useMemo(() => (Array.isArray(win.args.history) ? (win.args.history as string[]) : url ? [url] : []), [win.args.history, url])
  const at = typeof win.args.at === "number" ? win.args.at : history.length - 1
  const [address, setAddress] = useState(url)
  const [page, setPage] = useState<Page | null>(null)
  const [err, setErr] = useState("")
  const [loading, setLoading] = useState(false)
  const [reader, setReader] = useState<boolean>(readerDefault)
  const [tick, setTick] = useState(0)
  const input = useRef<HTMLInputElement>(null)

  useEffect(() => setAddress(url), [url])
  useEffect(() => {
    if (win.args.focus_address || !url) input.current?.focus()
  }, [win.args.focus_address, url])

  // Read the page on the server: its title, whether it may be framed, and the reader text.
  useEffect(() => {
    if (!url) return
    let live = true
    setLoading(true)
    setErr("")
    api
      .get<Page>(`read?url=${encodeURIComponent(url)}`)
      .then((p) => {
        if (!live) return
        setPage(p)
        void desk.setTitle(win.id, p.title || hostOf(url))
        desk.report(win.id, { title: p.title, reader: reader || !p.framable })
      })
      .catch((e: Error) => live && setErr(e.message))
      .finally(() => live && setLoading(false))
    return () => {
      live = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, tick, api])

  const go = async (to: string) => {
    if (!to.trim()) return
    const r = await api.get<{ url: string }>(`resolve?q=${encodeURIComponent(to)}`).catch((e: Error) => {
      setErr(e.message)
      return null
    })
    if (!r) return
    const hist = [...history.slice(0, at + 1), r.url].slice(-100)
    void desk.setArgs(win.id, { url: r.url, history: hist, at: hist.length - 1 })
  }
  const step = (d: number) => {
    const i = at + d
    if (i < 0 || i >= history.length) return
    void desk.setArgs(win.id, { url: history[i], history, at: i })
  }
  const submit = (e: FormEvent) => {
    e.preventDefault()
    void go(address)
  }

  const showReader = reader || (page !== null && !page.framable)
  const onReaderSelect = () => {
    const s = window.getSelection()?.toString().trim() ?? ""
    desk.report(win.id, { title: page?.title, reader: true, selection: s.slice(0, 2000) })
  }

  return (
    <div className="flex h-full min-h-0 flex-col text-sm">
      <form className="flex items-center gap-1 border-b border-black/10 px-2 py-1 dark:border-white/10" onSubmit={submit}>
        <button type="button" className={btn} disabled={at <= 0} title="Back" onClick={() => step(-1)}>
          <Icon name="arrow-left" size={15} />
        </button>
        <button type="button" className={btn} disabled={at >= history.length - 1} title="Forward" onClick={() => step(1)}>
          <Icon name="arrow-right" size={15} />
        </button>
        <button type="button" className={btn} title="Reload" onClick={() => setTick((t) => t + 1)}>
          <Icon name={loading ? "loader" : "rotate-cw"} size={15} className={loading ? "animate-spin" : ""} />
        </button>
        <button type="button" className={btn} title="Home" onClick={() => void go(home)}>
          <Icon name="house" size={15} />
        </button>
        <input
          ref={input}
          className="mx-1 min-w-0 flex-1 rounded border border-black/15 bg-transparent px-2 py-1 dark:border-white/15"
          value={address}
          placeholder="Search or enter an address"
          onChange={(e) => setAddress(e.target.value)}
          onFocus={(e) => e.target.select()}
        />
        <button
          type="button"
          className={`${btn} ${showReader ? "bg-black/10 dark:bg-white/15" : ""}`}
          disabled={page !== null && !page.framable}
          title={page && !page.framable ? "This site cannot be shown in a frame: reader mode" : "Reader mode"}
          onClick={() => {
            setReader(!reader)
            desk.report(win.id, { title: page?.title, reader: !reader })
          }}
        >
          <Icon name="book-open-text" size={15} />
        </button>
        {url && (
          <a className={btn} href={url} target="_blank" rel="noreferrer noopener" title="Open in the system browser">
            <Icon name="external-link" size={15} />
          </a>
        )}
      </form>
      {err && <div className="bg-red-500/10 px-3 py-1 text-xs">{err}</div>}
      <div className="min-h-0 flex-1">
        {!url && <div className="p-6 text-sm opacity-60">Type an address or words to search.</div>}
        {url && !showReader && (
          <iframe
            key={`${url}#${tick}`}
            className="h-full w-full border-0 bg-white"
            src={url}
            title={page?.title ?? url}
            sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-popups-to-escape-sandbox"
            referrerPolicy="no-referrer"
          />
        )}
        {url && showReader && page && (
          <article className="mx-auto h-full max-w-3xl overflow-auto px-6 py-4 leading-relaxed" onMouseUp={onReaderSelect}>
            <h1 className="mb-1 text-lg font-semibold">{page.title}</h1>
            <div className="mb-4 truncate text-xs opacity-60">{page.final_url}</div>
            {page.text.split("\n").map((l, i) => {
              const h = /^(#{1,6}) (.*)$/.exec(l)
              if (h) return <h2 key={i} className="mb-1 mt-4 font-semibold">{h[2]}</h2>
              if (!l.trim()) return null
              return (
                <p key={i} className="mb-2">
                  {l}
                </p>
              )
            })}
            {page.links && page.links.length > 0 && (
              <details className="mt-6 text-xs">
                <summary className="cursor-pointer opacity-70">Links ({page.links.length})</summary>
                <ul className="mt-2 space-y-1">
                  {page.links.map((l) => (
                    <li key={l.url}>
                      <button type="button" className="text-left underline" onClick={() => void go(l.url)}>
                        {l.text}
                      </button>{" "}
                      <span className="opacity-50">{hostOf(l.url)}</span>
                    </li>
                  ))}
                </ul>
              </details>
            )}
          </article>
        )}
      </div>
    </div>
  )
}

const web: ModuleWeb = { views: { "browser/main": BrowserView } }
export default web
