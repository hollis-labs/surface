/**
 * The component gallery: every component installed from the @surface registry, in its
 * variants and states, drawn from fixtures. Design work happens against it, and
 * `make ui-shots` photographs each section in dark and light. Nothing here calls the server.
 */
import type { ViewProps } from "@hollis-labs/surface-page/sdk"
import { useLayoutEffect, useRef, useState } from "react"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Ade } from "./ade"
import { Cards } from "./cards"
import { Chat } from "./chat"
import { Controls } from "./controls"
import { Display } from "./display"
import { Genui } from "./genui"
import { Overlays } from "./overlays"
import { DraftFilter } from "./Specimen"
import { Windows } from "./windows"

/** The sections, in order. Their ids name the screenshots. */
export const SECTIONS = [
  { id: "controls", title: "Controls", draw: Controls },
  { id: "display", title: "Display", draw: Display },
  { id: "overlays", title: "Overlays", draw: Overlays },
  { id: "chat", title: "Chat", draw: Chat },
  { id: "cards", title: "Cards", draw: Cards },
  { id: "genui", title: "GenUI", draw: Genui },
  { id: "ade", title: "ADE", draw: Ade },
  { id: "windows", title: "Windows", draw: Windows },
] as const

/**
 * A design job's draft on its own: every section marked as the job's draft (`<Section
 * draft="<job id>">`), wherever it lives in the gallery. A Design job's window opens here.
 */
function Draft({ job }: { job?: string }) {
  const box = useRef<HTMLDivElement>(null)
  const [empty, setEmpty] = useState(false)
  // Checked again whenever the sections change (hot reload while a designer marks them).
  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    const check = () => setEmpty(el.childElementCount === 0)
    check()
    const watch = new MutationObserver(check)
    watch.observe(el, { childList: true })
    return () => watch.disconnect()
  }, [])
  return (
    <>
      <div ref={box} className="contents">
        <DraftFilter.Provider value={{ only: true, job }}>
          {SECTIONS.map((s) => (
            <s.draw key={s.id} />
          ))}
        </DraftFilter.Provider>
      </div>
      {empty && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>No draft marked</EmptyTitle>
            <EmptyDescription>
              {job ? `No section is marked as the draft of ${job}` : "No section is marked as a draft"} (
              <code>{'<Section draft="<job id>">'}</code>). The whole gallery is in the other tabs.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
    </>
  )
}

export function Gallery({ win }: ViewProps) {
  return <GalleryTabs first={typeof win.args.section === "string" ? win.args.section : undefined} />
}

/**
 * The gallery's tabs, outside any window: the standalone page (standalone.tsx) draws this,
 * opening on `first` and telling `onSection` which section is shown.
 */
export function GalleryTabs({
  first,
  onSection,
  draft,
}: {
  first?: string
  onSection?: (id: string) => void
  /** Show the Draft tab first, for this design job (`true`: any job's marked draft). */
  draft?: string | true
}) {
  const tabs = draft ? [{ id: "draft", title: "Draft" }, ...SECTIONS] : SECTIONS
  const [section, setSection] = useState<string>(
    first && tabs.some((s) => s.id === first) ? first : draft ? "draft" : "controls",
  )
  return (
    <Tabs
      value={section}
      onValueChange={(v) => {
        setSection(String(v))
        onSection?.(String(v))
      }}
      className="@container flex h-full min-h-0 flex-col"
    >
      <div className="shrink-0 border-b border-border px-3 py-2">
        <TabsList variant="line" className="flex-wrap">
          {tabs.map((s) => (
            <TabsTrigger key={s.id} value={s.id} data-gallery-tab={s.id}>
              {s.title}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>
      <div className="min-h-0 flex-1 overflow-auto bg-muted/30">
        {draft && (
          <TabsContent value="draft" data-gallery-section="draft" className="flex flex-col gap-6 p-4">
            <Draft job={draft === true ? undefined : draft} />
          </TabsContent>
        )}
        {SECTIONS.map((s) => (
          <TabsContent key={s.id} value={s.id} data-gallery-section={s.id} className="flex flex-col gap-6 p-4">
            <s.draw />
          </TabsContent>
        ))}
      </div>
    </Tabs>
  )
}
