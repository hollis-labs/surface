/**
 * Browser: a page in a window. It is shown in a frame when the site allows that, else in
 * reader mode (the page's words and links as the server read them). History lives in the
 * window's args, so back and forward survive a reload and a saved layout.
 */
import { Icon, type ModuleWeb, useSetting, type ViewProps } from "@surface/sdk"
import { type FormEvent, useEffect, useMemo, useRef, useState } from "react"
import "./browser.css"

interface Page {
  url: string
  final_url: string
  title: string
  text: string
  links?: { text: string; url: string }[]
  framable: boolean
}

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

  useEffect(() => setAddress(url), [url])
  useEffect(() => {
    if (win.args.focus_address || !url) input.current?.focus()
  }, [win.args.focus_address, url])

  const showReader = reader || (page !== null && !page.framable)

  // Read the page on the server: its title, whether it may be framed, and the reader text.
  // Only a new address or a reload (tick) reads it again.
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick forces a reload; desk is read, not watched
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
      })
      .catch((e: Error) => live && setErr(e.message))
      .finally(() => live && setLoading(false))
    return () => {
      live = false
    }
  }, [url, tick, api])

  // desk.report replaces the window's state: report the whole of it.
  useEffect(() => {
    if (page) desk.report(win.id, { title: page.title, reader: showReader })
  }, [page, showReader, desk, win.id])

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
  const onReaderSelect = () => {
    const s = window.getSelection()?.toString().trim() ?? ""
    desk.report(win.id, { title: page?.title, reader: true, selection: s.slice(0, 2000) })
  }

  return (
    <div className="br">
      <form className="br-bar" onSubmit={submit}>
        <button type="button" className="br-ibtn" disabled={at <= 0} title="Back" onClick={() => step(-1)}>
          <Icon name="arrow-left" size={15} />
        </button>
        <button
          type="button"
          className="br-ibtn br-wide"
          disabled={at >= history.length - 1}
          title="Forward"
          onClick={() => step(1)}
        >
          <Icon name="arrow-right" size={15} />
        </button>
        <button type="button" className="br-ibtn" title="Reload" onClick={() => setTick((t) => t + 1)}>
          <Icon name={loading ? "loader" : "rotate-cw"} size={15} className={loading ? "br-spin" : undefined} />
        </button>
        <button type="button" className="br-ibtn br-wide" title="Home" onClick={() => void go(home)}>
          <Icon name="house" size={15} />
        </button>
        <input
          ref={input}
          className="sf-input br-addr"
          value={address}
          aria-label="Address"
          placeholder="Search or enter an address"
          onChange={(e) => setAddress(e.target.value)}
          onFocus={(e) => e.target.select()}
        />
        <button
          type="button"
          className="br-ibtn"
          data-on={showReader}
          disabled={page !== null && !page.framable}
          title={page && !page.framable ? "This site cannot be shown in a frame: reader mode" : "Reader mode"}
          onClick={() => setReader(!reader)}
        >
          <Icon name="book-open-text" size={15} />
        </button>
        {url && (
          <a
            className="br-ibtn br-wide"
            href={url}
            target="_blank"
            rel="noreferrer noopener"
            title="Open in the system browser"
          >
            <Icon name="external-link" size={15} />
          </a>
        )}
      </form>
      {err && <div className="br-error">{err}</div>}
      <div className="br-content">
        {!url && <p className="br-empty">Type an address or words to search.</p>}
        {url && !showReader && (
          <iframe
            key={`${url}#${tick}`}
            className="br-frame"
            src={url}
            title={page?.title ?? url}
            sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-popups-to-escape-sandbox"
            referrerPolicy="no-referrer"
          />
        )}
        {url && showReader && page && (
          <article className="br-reader" onMouseUp={onReaderSelect}>
            <h1>{page.title}</h1>
            <div className="br-source">{page.final_url}</div>
            {page.text.split("\n").map((l, i) => {
              // Paragraphs of a fixed text, keyed by position.
              const key = `p${i}`
              const h = /^(#{1,6}) (.*)$/.exec(l)
              if (h) return <h2 key={key}>{h[2]}</h2>
              if (!l.trim()) return null
              return <p key={key}>{l}</p>
            })}
            {page.links && page.links.length > 0 && (
              <details className="br-links">
                <summary>Links ({page.links.length})</summary>
                <ul>
                  {page.links.map((l) => (
                    <li key={l.url}>
                      <button type="button" className="br-link" onClick={() => void go(l.url)}>
                        {l.text}
                      </button>
                      <span className="br-host">{hostOf(l.url)}</span>
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
