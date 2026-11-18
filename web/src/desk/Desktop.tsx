/**
 * The desktop: a bar, the windows, the agent panel beside them, the palette over them.
 * Window state comes from the server (./store); this file draws it and turns what the
 * owner does into actions.
 */
import { Component, type ReactNode, useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Kbd } from "@/components/ui/kbd"
import { type WindowInfo, WindowManager, type WindowTree } from "@/components/ui/window-manager"
import { AgentPanel } from "../agent"
import { Icon, type ModuleWeb, useSetting } from "../sdk"
import { isConnected, onConnection } from "../sdk/runtime"
import { ConfirmHost } from "./Confirm"
import { clientCommands, runClient } from "./commands"
import { matches, parseKeys, showKeys } from "./keys"
import { moduleApi } from "./moduleApi"
import { Palette } from "./Palette"
import {
  act,
  app,
  type CommandDef,
  closeWindow,
  desk,
  runCommand,
  setClientRunner,
  toWin,
  useDeskState,
  viewById,
} from "./store"

export interface DesktopProps {
  /** The views compiled into this app's page, by module id. */
  modules: Record<string, ModuleWeb>
}

export function Desktop({ modules }: DesktopProps) {
  const state = useDeskState()
  const manifest = app()
  const [palette, setPalette] = useState(false)
  const [menu, setMenu] = useState(false)
  const [agentOpen, setAgentOpen] = useState(() => localStorage.getItem("surface.agent.open") !== "0")
  const [up, setUp] = useState(isConnected())
  const gap = (useSetting<number>("surface.gap") ?? 10) as number
  const theme = (useSetting<string>("surface.theme") ?? "dark") as string
  const panelWidth = (useSetting<number>("surface.agent_panel_width") ?? 420) as number

  useEffect(() => {
    setUp(isConnected())
    return onConnection(setUp)
  }, [])

  // The theme is a live setting: changing it redraws the desk at once, with no reload.
  useEffect(() => {
    const root = document.documentElement
    root.classList.toggle("dark", theme !== "light")
  }, [theme])

  useEffect(() => {
    document.title = manifest.title
  }, [manifest.title])

  const toggleAgent = useCallback((open?: boolean) => {
    setAgentOpen((was) => {
      const next = open ?? !was
      localStorage.setItem("surface.agent.open", next ? "1" : "0")
      return next
    })
  }, [])

  // The page's own commands (palette, panels) run here; the rest go to the server.
  const ui = useMemo(
    () => ({
      togglePalette: (o?: boolean) => setPalette((p) => o ?? !p),
      toggleAgent,
      toggleMenu: () => setMenu((m) => !m),
    }),
    [toggleAgent],
  )
  useEffect(() => {
    setClientRunner((id, args) => runClient(id, ui, args))
  }, [ui])

  const commands: CommandDef[] = useMemo(() => [...clientCommands, ...manifest.commands], [manifest.commands])

  // Shortcuts. A command's keys run it wherever the keyboard is, except plain typing keys.
  useEffect(() => {
    const chords = commands.flatMap((c) => {
      const chord = c.keys ? parseKeys(c.keys) : null
      return chord ? [{ chord, id: c.id }] : []
    })
    const onKey = (e: KeyboardEvent) => {
      for (const { chord, id } of chords) {
        if (matches(chord, e)) {
          e.preventDefault()
          e.stopPropagation()
          void runCommand(id).catch((err) => console.warn("command failed", id, err))
          return
        }
      }
    }
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  }, [commands])

  const windows: WindowInfo[] = useMemo(
    () =>
      state.wins.map((w) => ({
        id: w.id,
        title: w.title,
        dirty: w.dirty,
        icon: <Icon name={viewById(w.view)?.icon} size={13} />,
      })),
    [state.wins],
  )
  // The owner dragged, split, tabbed, floated or resized: the server keeps the result.
  const arrange = useCallback((tree: WindowTree, focus: string | undefined) => act({ type: "tree", tree, focus }), [])
  const menuViews = manifest.views.filter((v) => v.menu)

  return (
    <div className="fixed inset-0 flex flex-col bg-background text-sm" data-layout={state.layout}>
      <header className="flex h-9 shrink-0 select-none items-center justify-between gap-3 border-b bg-background px-2">
        <div className="flex h-full min-w-0 items-center gap-1">
          <DropdownMenu open={menu} onOpenChange={setMenu}>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="sm" title="Apps">
                  <Icon name="layout-grid" />
                  <span className="font-semibold">{manifest.title}</span>
                </Button>
              }
            />
            <DropdownMenuContent className="min-w-56">
              {menuViews.map((v) => (
                <DropdownMenuItem key={v.id} onClick={() => void desk.open(v.id)}>
                  <Icon name={v.icon} />
                  <span>{v.title}</span>
                  {v.module !== "surface" && <DropdownMenuShortcut>{v.module}</DropdownMenuShortcut>}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <div className="ml-1 flex min-w-0 gap-0.5 overflow-hidden">
            {state.wins.map((w) => (
              <Button
                key={w.id}
                variant={w.id === state.focus ? "secondary" : "ghost"}
                size="sm"
                className="min-w-0 max-w-44 shrink"
                title={w.title}
                aria-pressed={w.id === state.focus}
                onClick={() => void act({ type: "focus", id: w.id })}
                onAuxClick={(e) => {
                  if (e.button === 1) void closeWindow(w.id)
                }}
              >
                <Icon name={viewById(w.view)?.icon} />
                <span className="truncate">{w.dirty ? `● ${w.title}` : w.title}</span>
              </Button>
            ))}
          </div>
        </div>
        <div className="flex h-full shrink-0 items-center gap-1">
          {state.active && (
            <Button
              variant="ghost"
              size="sm"
              title="The saved layout in use"
              onClick={() => void desk.open("surface.layouts")}
            >
              <Icon name="layout-dashboard" />
              <span>{state.active}</span>
            </Button>
          )}
          {state.mode && (
            <Badge variant="secondary" title="Your mode">
              {state.mode}
            </Badge>
          )}
          <Button
            variant="ghost"
            size="sm"
            title="Tile every window, or float them all"
            onClick={() => void runCommand("layout.toggle")}
          >
            <Icon name={state.layout === "tile" ? "columns-2" : "picture-in-picture-2"} />
            <span>{state.layout}</span>
          </Button>
          <Button variant="ghost" size="sm" title="Command palette" onClick={() => setPalette(true)}>
            <Icon name="search" />
            <Kbd>{showKeys("cmd+k")}</Kbd>
          </Button>
          <Button
            variant={agentOpen ? "secondary" : "ghost"}
            size="icon-sm"
            title={`Agent ${showKeys("cmd+j")}`}
            aria-pressed={agentOpen}
            onClick={() => toggleAgent()}
          >
            <Icon name="bot" />
          </Button>
          <span
            className={`mx-1.5 size-2 rounded-full ${up ? "bg-success" : "bg-warning"}`}
            title={up ? "Connected" : "Reconnecting…"}
          />
        </div>
      </header>

      <div className="flex min-h-0 flex-1">
        {/* isolate: the windows' stacking order stays inside the canvas, under the bar and the overlays. */}
        <div
          className="relative isolate min-w-0 flex-1 overflow-hidden bg-muted/40"
          style={{ "--window-gap": `${gap}px` } as React.CSSProperties}
        >
          <WindowManager
            tree={state.tree}
            windows={windows}
            focus={state.focus}
            onArrange={arrange}
            onClose={(id) => void closeWindow(id)}
            empty={
              <Empty className="size-full">
                <EmptyHeader>
                  <EmptyTitle>{manifest.title}</EmptyTitle>
                  <EmptyDescription>
                    Open something from the menu, press <Kbd>{showKeys("cmd+k")}</Kbd> for the palette, or ask the
                    agent.
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            }
          >
            {(id) => <WindowBody id={id} modules={modules} />}
          </WindowManager>
        </div>
        {agentOpen && (
          <aside
            className="flex min-h-0 max-w-[60vw] shrink-0 flex-col overflow-hidden border-l bg-background"
            style={{ width: panelWidth }}
          >
            <AgentPanel desk={desk} title="Agent" />
          </aside>
        )}
      </div>

      <Palette open={palette} onOpenChange={setPalette} commands={commands} />
      <ConfirmHost />
    </div>
  )
}

/** One window's body: the view it shows, handed its window, the desk and its module's API. */
function WindowBody({ id, modules }: { id: string; modules: Record<string, ModuleWeb> }) {
  const state = useDeskState()
  const win = state.wins.find((w) => w.id === id)
  const focused = state.focus === id
  const module = win?.module ?? ""
  const api = useMemo(() => moduleApi(module), [module])
  const pub = useMemo(() => (win ? toWin(win, focused ? win.id : "") : null), [win, focused])
  if (!win || !pub) return null
  const view = viewById(win.view)
  const Cmp = view ? modules[view.module]?.views[view.render.entry] : undefined
  return (
    <div
      className="relative size-full min-h-0 overflow-auto data-[bare=true]:flex data-[bare=true]:flex-col data-[bare=true]:overflow-hidden"
      data-bare={!!view?.bare}
      data-window={win.id}
    >
      <Boundary key={win.view}>
        {Cmp ? (
          <Cmp win={pub} desk={desk} api={api} />
        ) : (
          <p className="p-4 text-muted-foreground">
            This app's page was built without the view “{win.view}”. Rebuild the page with the module's web entry.
          </p>
        )}
      </Boundary>
    </div>
  )
}

/** A view that throws takes down its own window's body, not the desk. */
class Boundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  render() {
    if (this.state.error) {
      return (
        <div className="flex flex-col items-start gap-3 p-4">
          <p>This window failed to draw.</p>
          <pre className="whitespace-pre-wrap text-destructive text-xs">{this.state.error.message}</pre>
          <Button variant="outline" size="sm" onClick={() => this.setState({ error: null })}>
            Try again
          </Button>
        </div>
      )
    }
    return this.props.children
  }
}
