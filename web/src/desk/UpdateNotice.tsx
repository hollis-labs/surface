/**
 * "A new version is running": shown when the event stream reconnects (the app's server
 * restarted, as on a deploy) and the server's page now loads a different bundle than this
 * tab did. The page's assets are content-hashed and its index is not cached, so comparing
 * the script it names is enough. Nothing happens in development (no hashed assets).
 */
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { onConnection } from "../sdk/runtime"

const bundleOf = (html: string): string | undefined => html.match(/\/assets\/[^"'\s>]+\.js/)?.[0]

function loadedBundle(): string | undefined {
  for (const el of document.querySelectorAll<HTMLScriptElement>("script[src]")) {
    const m = new URL(el.src, location.href).pathname.match(/\/assets\/[^"'\s>]+\.js$/)
    if (m) return m[0]
  }
  return undefined
}

export function UpdateNotice() {
  const [stale, setStale] = useState(false)
  useEffect(() => {
    const mine = loadedBundle()
    if (!mine) return
    let down = false
    return onConnection((up) => {
      if (!up) {
        down = true
        return
      }
      if (!down) return
      down = false
      void fetch("/", { cache: "no-store", credentials: "same-origin" })
        .then((r) => (r.ok ? r.text() : ""))
        .then((html) => {
          const now = bundleOf(html)
          if (now && now !== mine) setStale(true)
        })
        .catch(() => {})
    })
  }, [])
  if (!stale) return null
  return (
    <div
      role="status"
      data-slot="update-notice"
      className="fixed top-10 left-1/2 z-50 flex -translate-x-1/2 items-center gap-2 rounded-md border bg-card px-3 py-1.5 text-xs shadow-md"
    >
      <span>A new version of this app is running.</span>
      <Button size="xs" onClick={() => location.reload()}>
        Reload
      </Button>
      <Button size="xs" variant="ghost" onClick={() => setStale(false)}>
        Later
      </Button>
    </div>
  )
}
