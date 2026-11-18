/**
 * The gallery on its own page, with no app server: what `make design-preview` serves from a
 * checkout and what a Design job's window frames.
 *
 *   gallery.html?theme=dark|light#<section>
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
document.documentElement.classList.toggle("dark", theme === "dark")

const fromHash = () => {
  const h = decodeURIComponent(window.location.hash.slice(1))
  return SECTIONS.some((s) => s.id === h) ? h : "controls"
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
