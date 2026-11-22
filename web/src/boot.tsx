/**
 * Booting a Surface page: load the app's manifest and settings from its server, open the event
 * stream, then draw the desktop and the cards over it. Every app's page starts here, whether it is
 * built in this repository (main.tsx) or installs @hollis-labs/surface-page.
 */
import { StrictMode, useEffect, useState } from "react"
import { createRoot, type Root } from "react-dom/client"
import { MarkdownExtensions } from "@/components/ui/markdown"
import { AuthGate } from "./auth/gate"
import { registerCard } from "./cards/renderers"
import { CardHost } from "./cards/views"
import { CiteDrawer } from "./cite/record.tsx"
import { initCite } from "./cite/store.ts"
import { CiteFloat } from "./cite/terminal.tsx"
import { citeMarkdownExtensions } from "./cite/ui.tsx"
import { coreWeb } from "./desk/coreViews"
import { Desktop } from "./desk/Desktop"
import { moduleApi } from "./desk/moduleApi"
import { loadManifest } from "./desk/store"
import { call, connect, loadSettings } from "./sdk/runtime"
import type { ModuleWeb } from "./sdk/types"

const registered = new Set<string>()

/** Registers the card renderers the modules bring (ModuleWeb.cards), once per kind. */
function registerModuleCards(modules: Record<string, ModuleWeb>) {
  for (const [id, m] of Object.entries(modules)) {
    for (const [key, Cmp] of Object.entries(m.cards ?? {})) {
      const [kind, version = "1"] = key.split("@")
      if (registered.has(`${kind}@${version}`)) continue
      registered.add(`${kind}@${version}`)
      const api = moduleApi(id)
      registerCard(kind, version, ({ card, respond }) => <Cmp card={card} respond={respond} api={api} />)
    }
  }
}

export interface PageProps {
  /** The views compiled into this app's page, by module id. */
  modules: Record<string, ModuleWeb>
  /** Views of the core's own (`surface/*`) to add, such as the Components window from ./gallery. */
  core?: ModuleWeb["views"]
  /**
   * The app's name and title (the generated module list exports them as `app`), for the
   * sign-in screens of an app with sign-in, which draw before the server says who it is.
   * Default: the document's title.
   */
  app?: { name: string; title: string }
}

/** The whole page: a loading state until the server answers, then the desktop and the cards. */
export function Page({ modules, core }: PageProps) {
  registerModuleCards(modules)
  const [ready, setReady] = useState(false)
  const [error, setError] = useState("")
  useEffect(() => {
    let live = true
    Promise.all([loadManifest(), loadSettings(), initCite()])
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
  // References in every Markdown on the page are chips (web/src/cite). The value is a
  // module-level constant, so the blocks are not redrawn.
  return (
    <MarkdownExtensions value={citeMarkdownExtensions}>
      <Desktop modules={{ ...modules, surface: { ...coreWeb, views: { ...coreWeb.views, ...core } } }} />
      <CardHost />
      <CiteDrawer />
      <CiteFloat />
    </MarkdownExtensions>
  )
}

/** Draw the page into an element (by default `#root`). */
export function mountPage(props: PageProps, element: Element | null = document.getElementById("root")): Root {
  if (!element) throw new Error("mountPage: no element to draw into (#root not found)")
  const root = createRoot(element)
  const title = props.app?.title ?? document.title
  const name = props.app?.name ?? title.toLowerCase().split(/\s+/)[0]
  // With sign-in (surface.yaml auth: identity) nothing of the desk loads until someone has
  // signed in; without it the gate renders the page at once.
  root.render(
    <StrictMode>
      <AuthGate name={name} title={title}>
        <Page {...props} />
      </AuthGate>
    </StrictMode>,
  )
  return root
}
