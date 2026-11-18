import { StrictMode, useEffect, useState } from "react"
import { createRoot } from "react-dom/client"
import { modules } from "@surface/app"
import { coreWeb } from "./desk/coreViews"
import { Desktop } from "./desk/Desktop"
import { loadManifest } from "./desk/store"
import { call, connect, loadSettings } from "./sdk/runtime"
import "./index.css"

function Boot() {
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
  if (error) return <div className="sf-boot">The app's server did not answer: {error}</div>
  if (!ready) return <div className="sf-boot">…</div>
  return <Desktop modules={{ ...modules, surface: coreWeb }} />
}

const root = document.getElementById("root")
if (!root) throw new Error("root element #root not found")
createRoot(root).render(
  <StrictMode>
    <Boot />
  </StrictMode>,
)
