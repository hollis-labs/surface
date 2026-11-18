import { useEffect, useState } from "react"
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@/components/ui/command"
import { Icon } from "../sdk"
import { showKeys } from "./keys"
import { app, type CommandDef, type LayoutDef, listLayouts, loadLayout, runCommand } from "./store"

interface Row {
  id: string
  title: string
  section: string
  icon?: string
  hint?: string
  keywords: string[]
  run: () => unknown
}

interface PaletteProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  commands: CommandDef[]
}

/** The command palette: every command, every view and every saved layout, by what you type. */
export function Palette({ open, onOpenChange, commands }: PaletteProps) {
  const [layouts, setLayouts] = useState<LayoutDef[]>([])
  // Layouts are read each time the palette opens, so one saved a moment ago is there.
  useEffect(() => {
    if (!open) return
    void listLayouts()
      .then(setLayouts)
      .catch(() => {})
  }, [open])

  const rows: Row[] = []
  for (const c of commands) {
    if (c.hidden) continue
    const viewId = c.id.startsWith("open.") ? c.id.slice(5) : ""
    rows.push({
      id: c.id,
      title: c.title,
      section: c.section ?? "Commands",
      icon: viewId ? app().views.find((v) => v.id === viewId)?.icon : undefined,
      hint: showKeys(c.keys),
      keywords: [c.keywords ?? "", c.section ?? "", c.id],
      run: () => runCommand(c.id),
    })
  }
  for (const l of layouts) {
    rows.push({
      id: `layout:${l.name}`,
      title: `Layout: ${l.name}`,
      section: "Layouts",
      icon: "layout-dashboard",
      keywords: ["layout", "arrange", "saved", l.about ?? ""],
      run: () => loadLayout(l.name),
    })
  }
  const sections = [...new Set(rows.map((r) => r.section))]

  const pick = (row: Row) => {
    onOpenChange(false)
    void Promise.resolve(row.run()).catch((err) => console.warn("palette: failed", row.id, err))
  }

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Command palette"
      description="Run a command, open a window or load a layout."
    >
      <Command>
        <CommandInput placeholder="Run a command, open a window, load a layout…" />
        <CommandList>
          <CommandEmpty>Nothing matches.</CommandEmpty>
          {sections.map((section) => (
            <CommandGroup key={section} heading={section}>
              {rows
                .filter((r) => r.section === section)
                .map((r) => (
                  <CommandItem key={r.id} value={r.title} keywords={r.keywords} onSelect={() => pick(r)}>
                    <Icon name={r.icon ?? "chevron-right"} />
                    <span>{r.title}</span>
                    {r.hint && <CommandShortcut>{r.hint}</CommandShortcut>}
                  </CommandItem>
                ))}
            </CommandGroup>
          ))}
        </CommandList>
      </Command>
    </CommandDialog>
  )
}
