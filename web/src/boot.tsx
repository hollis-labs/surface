/**
 * Booting a Surface page: load the app's manifest and settings from its server, open the event
 * stream, then draw the desktop and the cards over it. Every app's page starts here, whether it is
 * built in this repository (main.tsx) or installs @hollis-labs/surface-page.
 */
import { StrictMode, useEffect, useState } from "react"
import { createRoot, type Root } from "react-dom/client"
import { CardHost } from "./cards/views"
import { coreWeb } from "./desk/coreViews"
import { Desktop } from "./desk/Desktop"
import { loadManifest } from "./desk/store"
import { call, connect, loadSettings } from "./sdk/runtime"
import type { ModuleWeb } from "./sdk/types"

export interface PageProps {
  /** The views compiled into this app's page, by module id. */
  modules: Record<string, ModuleWeb>
  /** Views of the core's own (`surface/*`) to add, such as the Components window from ./gallery. */
  core?: ModuleWeb["views"]
}

/** The whole page: a loading state until the server answers, then the desktop and the cards. */
export function Page({ modules, core }: PageProps) {
  const [ready, setReady] = useState(false)
  const [error, setError] = useState("")
  useEffect(() => {
    let live = true
    Promise.all([loadManifest(), loadSettings()])
      .then(() => {
        if (!live) return
        connect()
        setReady(true)
        void call("POST", "/api/events", { topic: "page.ready", data: {} }).catch(() => {})
      })
      .catch((err: Error) => live && setError(err.message))
    return () => {
      live = false
    }
  }, [])
  if (error)
    return (
      <div className="grid h-full place-items-center text-muted-foreground">
        The app's server did not answer: {error}
      </div>
    )
  if (!ready) return <div className="grid h-full place-items-center text-muted-foreground">…</div>
  return (
    <>
      <Desktop modules={{ ...modules, surface: { ...coreWeb, views: { ...coreWeb.views, ...core } } }} />
      <CardHost />
    </>
  )
}

/** Draw the page into an element (by default `#root`). */
export function mountPage(props: PageProps, element: Element | null = document.getElementById("root")): Root {
  if (!element) throw new Error("mountPage: no element to draw into (#root not found)")
  const root = createRoot(element)
  root.render(
    <StrictMode>
      <Page {...props} />
    </StrictMode>,
  )
  return root
}
