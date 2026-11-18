/**
 * The component gallery: every component installed from the @surface registry, in its
 * variants and states, drawn from fixtures. Design work happens against it, and
 * `make ui-shots` photographs each section in dark and light. Nothing here calls the server.
 */
import type { ViewProps } from "@hollis-labs/surface-page/sdk"
import { useState } from "react"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Ade } from "./ade"
import { Cards } from "./cards"
import { Chat } from "./chat"
import { Controls } from "./controls"
import { Display } from "./display"
import { Genui } from "./genui"
import { Overlays } from "./overlays"
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

export function Gallery({ win }: ViewProps) {
  return <GalleryTabs first={typeof win.args.section === "string" ? win.args.section : undefined} />
}

/**
 * The gallery's tabs, outside any window: the standalone page (standalone.tsx) draws this,
 * opening on `first` and telling `onSection` which section is shown.
 */
export function GalleryTabs({ first, onSection }: { first?: string; onSection?: (id: string) => void }) {
  const [section, setSection] = useState<string>(first && SECTIONS.some((s) => s.id === first) ? first : "controls")
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
          {SECTIONS.map((s) => (
            <TabsTrigger key={s.id} value={s.id} data-gallery-tab={s.id}>
              {s.title}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>
      <div className="min-h-0 flex-1 overflow-auto bg-muted/30">
        {SECTIONS.map((s) => (
          <TabsContent key={s.id} value={s.id} data-gallery-section={s.id} className="flex flex-col gap-6 p-4">
            <s.draw />
          </TabsContent>
        ))}
      </div>
    </Tabs>
  )
}
