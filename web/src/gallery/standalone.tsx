/**
 * The gallery on its own page, with no app server: what `make design-preview` serves from a
 * checkout and what a Design job's window frames.
 *
 *   gallery.html?theme=dark|light[&job=<design job id>|&draft]#<section>
 *
 * With job= (or draft), the Draft tab comes first and is where the page opens: only the
 * sections marked as that job's draft (<Section draft="<job id>">).
 *
 * The hash follows the shown section both ways. A page that frames this one is told the
 * section and the theme with postMessage ({ source: "surface-gallery", section, theme }), so a
 * note pinned beside it knows where it points.
 */
import "../index.css"
import { StrictMode, useEffect, useState } from "react"
import { createRoot } from "react-dom/client"
import { GalleryTabs, SECTIONS } from "./Gallery"

const params = new URLSearchParams(window.location.search)
const theme = params.get("theme") === "light" ? "light" : "dark"
const job = params.get("job") ?? undefined
const draft: string | true | undefined = job || (params.has("draft") ? true : undefined)
document.documentElement.classList.toggle("dark", theme === "dark")

const fromHash = () => {
  const h = decodeURIComponent(window.location.hash.slice(1))
  if (draft && (h === "" || h === "draft")) return "draft"
  return SECTIONS.some((s) => s.id === h) ? h : draft ? "draft" : "workflows"
}

const tell = (section: string) => {
  if (window.parent !== window) window.parent.postMessage({ source: "surface-gallery", section, theme }, "*")
}

function Standalone() {
  const [section, setSection] = useState(fromHash)
  useEffect(() => {
    const onHash = () => setSection(fromHash())
    window.addEventListener("hashchange", onHash)
    return () => window.removeEventListener("hashchange", onHash)
  }, [])
  useEffect(() => tell(section), [section])
  return (
    <div className="h-full bg-background text-foreground">
      {/* Keyed by section: a new hash opens the gallery there. */}
      <GalleryTabs
        key={section}
        first={section}
        draft={draft}
        onSection={(id) => {
          window.history.replaceState(null, "", `#${id}`)
          tell(id)
        }}
      />
    </div>
  )
}

const root = document.getElementById("root")
if (!root) throw new Error("no #root")
createRoot(root).render(
  <StrictMode>
    <Standalone />
  </StrictMode>,
)
