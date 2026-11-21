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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
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
  viewById,
} from "./store"

const RECENT_KEY = "surface.desk.palette.recent"

function readRecents(): string[] {
  try {
    return JSON.parse(window.localStorage.getItem(RECENT_KEY) ?? "[]") as string[]
  } catch {
    return []
  }
}

function rememberRecent(id: string) {
  try {
    const next = [id, ...readRecents().filter((r) => r !== id)].slice(0, 8)
    window.localStorage.setItem(RECENT_KEY, JSON.stringify(next))
  } catch {
    // Private window.
  }
}

interface Row {
  id: string
  title: string
  subtitle?: string
  section: string
  icon?: string
  hint?: string
  keywords: string[]
  close?: () => void
  run: () => unknown
}

interface PaletteProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  commands: CommandDef[]
}

/** The command palette: mode tabs (Tabs | Commands) on top, search on its own line below, then grouped rows. */
export function Palette({ open, onOpenChange, commands }: PaletteProps) {
  const state = useDeskState()
  const [mode, setMode] = useState<"tabs" | "commands">("commands")
  const [scope, setScope] = useState<"all" | "focused">("all")
  const [query, setQuery] = useState("")
  const [providers, setProviders] = useState<PaletteProvider[]>([])
  const [groups, setGroups] = useState<Record<string, PaletteGroup>>({})
  const [error, setError] = useState("")
  const [layouts, setLayouts] = useState<LayoutDef[]>([])
  const [recents, setRecents] = useState<string[]>(readRecents)
  const deskRevision = mode === "tabs" ? state.rev : 0

  useEffect(() => {
    if (!open) return
    let live = true
    setError("")
    setQuery("")
    setRecents(readRecents())
    const focused = state.wins.find((w) => w.id === state.focus)
    setMode(focused?.view === "browser" ? "tabs" : "commands")
    void listLayouts()
      .then((l) => {
        if (live) setLayouts(l)
      })
      .catch(() => {})
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
  }, [open, state.focus, state.wins])

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

  const windowRows: Row[] = state.wins
    .filter((w) => (scope === "focused" ? w.id === state.focus : true))
    .filter((w) => matchesPalette(query, w.title, w.view, w.module, String(w.args?.url ?? "")))
    .map((w) => {
      const v = viewById(w.view)
      const url = typeof w.args?.url === "string" ? w.args.url : undefined
      return {
        id: `window.focus.${w.id}`,
        title: w.title,
        subtitle: url ?? (v && v.title !== w.title ? v.title : w.module),
        section: mode === "tabs" ? "Open windows & tabs" : "Open windows",
        icon: v?.icon ?? "app-window",
        keywords: ["window", "tab", "focus", "switch", w.title, w.view, w.module, url ?? ""],
        close: () => void closeWindow(w.id),
        run: () => act({ type: "focus", id: w.id }),
      }
    })

  const commandRows: Row[] = []
  for (const c of mode === "commands" ? commands : []) {
    if (c.hidden || !matchesPalette(query, c.title, c.keywords ?? "", c.section ?? "")) continue
    const viewId = c.id.startsWith("open.") ? c.id.slice(5) : ""
    commandRows.push({
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
    commandRows.push({
      id: `layout:${l.name}`,
      title: `Layout: ${l.name}`,
      subtitle: l.about,
      section: "Layouts",
      icon: "layout-dashboard",
      keywords: ["layout", "arrange", "saved", l.about ?? ""],
      run: () => loadLayout(l.name),
    })
  }

  const baseRows = mode === "tabs" ? windowRows : [...windowRows, ...commandRows]

  const recentRows: Row[] =
    mode === "commands" && !query.trim()
      ? recents
          .map((id) => baseRows.find((r) => r.id === id))
          .filter((r): r is Row => Boolean(r))
          .map((r) => ({ ...r, section: "Recent" }))
      : []

  const remainingRows =
    mode === "commands" && !query.trim() ? baseRows.filter((r) => !recents.includes(r.id)) : baseRows
  const rows = [...recentRows, ...remainingRows]
  const sections = [...new Set(rows.map((r) => r.section))]

  const pick = (row: Row) => {
    rememberRecent(row.id)
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

  const switchMode = (next: "tabs" | "commands") => {
    setMode(next)
    requestAnimationFrame(() => {
      document.querySelector<HTMLInputElement>('[data-slot="command-input"]')?.focus()
    })
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
        {/* Top Mode Bar (Tabs | Commands), matching Basalisk's desk-palette-modes */}
        <div className="flex items-center gap-1.5 border-b border-border px-3.5 py-2">
          <Button
            variant="outline"
            size="xs"
            tabIndex={-1}
            aria-pressed={mode === "tabs"}
            className={`h-6 rounded-sm px-2.5 font-mono text-xs ${
              mode === "tabs"
                ? "border-primary/50 bg-accent text-primary"
                : "border-border bg-card text-muted-foreground hover:text-foreground"
            }`}
            onClick={() => switchMode("tabs")}
          >
            Tabs
          </Button>
          <Button
            variant="outline"
            size="xs"
            tabIndex={-1}
            aria-pressed={mode === "commands"}
            className={`h-6 rounded-sm px-2.5 font-mono text-xs ${
              mode === "commands"
                ? "border-primary/50 bg-accent text-primary"
                : "border-border bg-card text-muted-foreground hover:text-foreground"
            }`}
            onClick={() => switchMode("commands")}
          >
            Commands
          </Button>
          {mode === "tabs" && (
            <Select
              value={scope}
              onValueChange={(v) => {
                setScope((v as "all" | "focused") ?? "all")
                requestAnimationFrame(() => {
                  document.querySelector<HTMLInputElement>('[data-slot="command-input"]')?.focus()
                })
              }}
              items={[
                { value: "all", label: "All windows" },
                { value: "focused", label: "Focused window" },
              ]}
            >
              <SelectTrigger tabIndex={-1} size="sm" aria-label="Tab scope" className="ml-auto h-6 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All windows</SelectItem>
                <SelectItem value="focused">Focused window</SelectItem>
              </SelectContent>
            </Select>
          )}
        </div>

        {/* Dedicated Search Line */}
        <CommandInput
          autoFocus
          value={query}
          onValueChange={setQuery}
          placeholder={
            mode === "tabs" ? "Search open Browser tabs…" : "Search commands, files, notes and cited records…"
          }
          onKeyDown={(e) => {
            if (e.key === "Tab") {
              e.preventDefault()
              switchMode(mode === "tabs" ? "commands" : "tabs")
            }
          }}
        />
        {error && (
          <p role="alert" className="px-3 py-1 text-destructive text-xs">
            {error}
          </p>
        )}
        {loading && (
          <p role="status" className="px-3 py-1 text-muted-foreground text-xs">
            Searching…
          </p>
        )}
        {activeProviders.map(
          (p) =>
            groups[p.id]?.error && (
              <p key={p.id} role="status" className="px-3 py-1 text-destructive text-xs">
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
                  <CommandItem
                    key={`${section}:${r.id}`}
                    value={`${section}:${r.id}`}
                    keywords={r.keywords}
                    onSelect={() => pick(r)}
                  >
                    <Icon name={r.icon ?? "chevron-right"} />
                    <span className="truncate font-medium text-foreground">{r.title}</span>
                    {r.subtitle && (
                      <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{r.subtitle}</span>
                    )}
                    {r.hint && <CommandShortcut>{r.hint}</CommandShortcut>}
                    {mode === "tabs" && r.close && (
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        className="ml-auto size-5 text-muted-foreground hover:bg-destructive/20 hover:text-destructive"
                        aria-label={`Close ${r.title}`}
                        onClick={(e) => {
                          e.stopPropagation()
                          r.close?.()
                        }}
                      >
                        <Icon name="x" size={12} />
                      </Button>
                    )}
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

        <footer className="flex items-center justify-end gap-3.5 border-t border-border px-3.5 py-1.5 font-mono text-[11px] text-muted-foreground">
          <span>↑↓ move</span>
          <span>↩ {mode === "tabs" ? "switch" : "run"}</span>
          <span>esc close</span>
        </footer>
      </Command>
    </CommandDialog>
  )
}
