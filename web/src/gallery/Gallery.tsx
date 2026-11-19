/**
 * The component gallery: every component installed from the @surface registry, in its
 * variants and states, drawn from fixtures. Design work happens against it, and
 * `make ui-shots` photographs each section in dark and light. Nothing here calls the server.
 */
import type { ViewProps } from "@hollis-labs/surface-page/sdk"
import { PALETTES, type Palette, paletteById, type TypeStyle } from "@hollis-labs/surface-theme"
import { useLayoutEffect, useMemo, useRef, useState } from "react"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Ade } from "./ade"
import { Cards } from "./cards"
import { Chat } from "./chat"
import { Controls } from "./controls"
import { Display } from "./display"
import { Genui } from "./genui"
import { Overlays } from "./overlays"
import { DraftFilter } from "./Specimen"
import { InPalette, Theme, ThemeChoice } from "./theme"
import { Windows } from "./windows"

/** The sections, in order. Their ids name the screenshots. */
export const SECTIONS = [
  { id: "theme", title: "Theme", draw: Theme },
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
  // The palette and type style the gallery is drawn in: from the URL (?palette=, ?type=), else as last
  // chosen in this browser, else the default. The page around it keeps its own.
  const [palette, setPalette] = useState<Palette>(
    () => paletteById(remembered("palette") ?? "") ?? (PALETTES[0] as Palette),
  )
  const [type, setType] = useState<TypeStyle>(() => (remembered("type") === "mono" ? "mono" : "mixed"))
  const choice = useMemo(
    () => ({
      palette,
      type,
      setPalette: (p: Palette) => {
        setPalette(p)
        remember("palette", p.id)
      },
      setType: (t: TypeStyle) => {
        setType(t)
        remember("type", t)
      },
    }),
    [palette, type],
  )
  return (
    <ThemeChoice.Provider value={choice}>
      <InPalette palette={palette} type={type} className="h-full min-h-0">
        <Tabs
          value={section}
          onValueChange={(v) => {
            setSection(String(v))
            onSection?.(String(v))
          }}
          className="@container flex h-full min-h-0 flex-col"
        >
          <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border px-3 py-2">
            <TabsList variant="line" className="flex-wrap">
              {tabs.map((s) => (
                <TabsTrigger key={s.id} value={s.id} data-gallery-tab={s.id}>
                  {s.title}
                </TabsTrigger>
              ))}
            </TabsList>
            <div className="ml-auto flex items-center gap-2">
              <Select
                value={palette.id}
                onValueChange={(v) => {
                  const p = paletteById(String(v))
                  if (p) choice.setPalette(p)
                }}
                items={PALETTES.map((p) => ({ value: p.id, label: p.name }))}
              >
                <SelectTrigger size="sm" aria-label="Palette">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PALETTES.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <ToggleGroup
                size="sm"
                spacing={0}
                value={[type]}
                onValueChange={(v: string[]) => v[0] && choice.setType(v[0] as TypeStyle)}
              >
                <ToggleGroupItem value="mixed" aria-label="Mono chrome, sans body">
                  Mixed
                </ToggleGroupItem>
                <ToggleGroupItem value="mono" aria-label="Mono everywhere">
                  Mono
                </ToggleGroupItem>
              </ToggleGroup>
            </div>
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
      </InPalette>
    </ThemeChoice.Provider>
  )
}

/** The gallery's choice, from the URL first, then this browser. */
function remembered(key: "palette" | "type"): string | undefined {
  const fromURL = new URLSearchParams(window.location.search).get(key)
  if (fromURL) return fromURL
  try {
    return localStorage.getItem(`surface.gallery.${key}`) ?? undefined
  } catch {
    return undefined
  }
}

function remember(key: "palette" | "type", value: string) {
  try {
    localStorage.setItem(`surface.gallery.${key}`, value)
  } catch {
    // A private window: the choice lasts until reload.
  }
}
