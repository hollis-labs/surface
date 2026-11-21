import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
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
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { openCite } from "../cite"
import { Icon } from "../sdk"
import { call } from "../sdk/runtime"
import { showKeys } from "./keys"
import {
  fetchPaletteRows,
  matchesPalette,
  type PaletteGroup,
  type PaletteProvider,
  type PaletteResult,
  searchProviders,
} from "./palette-search"
import {
  act,
  app,
  type CommandDef,
  closeWindow,
  type LayoutDef,
  listLayouts,
  loadLayout,
  runCommand,
  useDeskState,
} from "./store"

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
  const state = useDeskState()
  const [mode, setMode] = useState("commands")
  const [scope, setScope] = useState("all")
  const [query, setQuery] = useState("")
  const [providers, setProviders] = useState<PaletteProvider[]>([])
  const [groups, setGroups] = useState<Record<string, PaletteGroup>>({})
  const [error, setError] = useState("")
  const deskRevision = mode === "tabs" ? state.rev : 0
  useEffect(() => {
    if (!open) return
    let live = true
    setError("")
    void call<PaletteProvider[]>("GET", "/api/palette/providers").then(
      (p) => {
        if (live) setProviders(p)
      },
      (e: Error) => {
        if (live) setError(e.message)
      },
    )
    return () => {
      live = false
    }
  }, [open])
  // Debounce typed queries. Desk changes refresh Tabs so closes/focuses made elsewhere appear.
  // biome-ignore lint/correctness/useExhaustiveDependencies: deskRevision invalidates live tab results
  useEffect(() => {
    setGroups({})
    if (!open || (mode === "commands" && !query.trim())) return
    let cancel = () => {}
    const timer = setTimeout(
      () => {
        cancel = searchProviders(
          providers.filter((p) => p.mode === mode),
          (p, signal) => fetchPaletteRows(p, query, scope, signal),
          (id, result) => setGroups((old) => ({ ...old, [id]: result })),
        )
      },
      mode === "tabs" ? 0 : 180,
    )
    return () => {
      clearTimeout(timer)
      cancel()
    }
  }, [open, providers, mode, query, scope, deskRevision])
  const [layouts, setLayouts] = useState<LayoutDef[]>([])
  // Layouts are read each time the palette opens, so one saved a moment ago is there.
  useEffect(() => {
    if (!open) return
    void listLayouts()
      .then(setLayouts)
      .catch(() => {})
  }, [open])

  const rows: Row[] = []
  for (const c of mode === "commands" ? commands : []) {
    if (c.hidden || !matchesPalette(query, c.title, c.keywords ?? "", c.section ?? "")) continue
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
  for (const l of mode === "commands" ? layouts : []) {
    if (!matchesPalette(query, l.name, l.about ?? "", "layout")) continue
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

  const pickResult = (row: PaletteResult) => {
    onOpenChange(false)
    if (row.ref) {
      openCite(row.ref)
      return
    }
    const action = row.window
      ? act({ type: "focus", id: row.window })
      : row.command
        ? runCommand(row.command.command, row.command.args)
        : Promise.resolve()
    void action.catch((e: Error) => setError(e.message))
  }
  const activeProviders = providers.filter((p) => p.mode === mode)
  const loading = Object.values(groups).some((g) => g.loading)
  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Command palette"
      description="Search commands, files, notes, cited records or open Browser tabs."
    >
      <Command shouldFilter={false}>
        <div className="flex items-center gap-2 p-2">
          <ToggleGroup value={[mode]} onValueChange={(v: string[]) => v[0] && setMode(v[0])} aria-label="Palette mode">
            <ToggleGroupItem value="commands">Commands</ToggleGroupItem>
            <ToggleGroupItem value="tabs">Tabs</ToggleGroupItem>
          </ToggleGroup>
          {mode === "tabs" && (
            <ToggleGroup value={[scope]} onValueChange={(v: string[]) => v[0] && setScope(v[0])} aria-label="Tab scope">
              <ToggleGroupItem value="all">All windows</ToggleGroupItem>
              <ToggleGroupItem value="focused">Focused window</ToggleGroupItem>
            </ToggleGroup>
          )}
        </div>
        <CommandInput
          value={query}
          onValueChange={setQuery}
          placeholder={
            mode === "tabs" ? "Search open Browser tabs…" : "Search commands, files, notes and cited records…"
          }
        />
        {error && (
          <p role="alert" className="px-3 py-1 text-destructive text-sm">
            {error}
          </p>
        )}
        {loading && (
          <p role="status" className="px-3 py-1 text-muted-foreground text-sm">
            Searching…
          </p>
        )}
        {activeProviders.map(
          (p) =>
            groups[p.id]?.error && (
              <p key={p.id} role="status" className="px-3 py-1 text-destructive text-sm">
                {p.title}: {groups[p.id].error}
              </p>
            ),
        )}
        <CommandList>
          <CommandEmpty>Nothing matches.</CommandEmpty>
          {sections.map((section) => (
            <CommandGroup key={section} heading={section}>
              {rows
                .filter((r) => r.section === section)
                .map((r) => (
                  <CommandItem key={r.id} value={r.id} keywords={r.keywords} onSelect={() => pick(r)}>
                    <Icon name={r.icon ?? "chevron-right"} />
                    <span>{r.title}</span>
                    {r.hint && <CommandShortcut>{r.hint}</CommandShortcut>}
                  </CommandItem>
                ))}
            </CommandGroup>
          ))}
          {activeProviders.map(
            (p) =>
              (groups[p.id]?.rows.length ?? 0) > 0 && (
                <CommandGroup key={p.id} heading={p.title}>
                  {groups[p.id].rows.map((row) => (
                    <CommandItem key={row.id} value={`${p.id}:${row.id}`} onSelect={() => pickResult(row)}>
                      <Icon name={row.icon ?? "search"} />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate">{row.title}</span>
                        {row.description && (
                          <span className="block truncate text-muted-foreground text-xs">{row.description}</span>
                        )}
                      </span>
                      {row.window && row.close && (
                        <Button
                          size="icon-xs"
                          variant="ghost"
                          aria-label={`Close ${row.title}`}
                          onKeyDown={(e) => {
                            if (e.key === "Enter" || e.key === " ") e.stopPropagation()
                          }}
                          onClick={(e) => {
                            e.stopPropagation()
                            void closeWindow(row.window as string).catch((err: Error) => setError(err.message))
                          }}
                        >
                          <Icon name="x" />
                        </Button>
                      )}
                    </CommandItem>
                  ))}
                </CommandGroup>
              ),
          )}
        </CommandList>
      </Command>
    </CommandDialog>
  )
}
