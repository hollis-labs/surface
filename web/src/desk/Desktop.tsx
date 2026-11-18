/**
 * The desktop: a bar, the windows, the agent panel beside them, the palette over them.
 * Window state comes from the server (./store); this file draws it and turns what the
 * owner does into actions.
 */
import { Component, type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react"
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
import { AgentPanel } from "../agent"
import { Icon, type ModuleWeb, useSetting } from "../sdk"
import { isConnected, onConnection } from "../sdk/runtime"
import { ConfirmHost } from "./Confirm"
import { clientCommands, runClient } from "./commands"
import { matches, parseKeys, showKeys } from "./keys"
import { MIN_H, MIN_W, type Placed, place, type Rect } from "./layout"
import { moduleApi } from "./moduleApi"
import { Palette } from "./Palette"
import {
  act,
  app,
  type CommandDef,
  closeWindow,
  type DeskWin,
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
  const canvasRef = useRef<HTMLDivElement>(null)
  const [area, setArea] = useState<Rect>({ x: 0, y: 0, w: 1200, h: 800 })

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

  useEffect(() => {
    const el = canvasRef.current
    if (!el) return
    const measure = () => setArea({ x: 0, y: 0, w: el.clientWidth, h: el.clientHeight })
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

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

  const placed = useMemo(() => place(state, area, gap), [state, area, gap])
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
                title={w.minimized ? `${w.title} (hidden)` : w.title}
                aria-pressed={w.id === state.focus}
                onClick={() => void act({ type: "focus", id: w.id })}
                onAuxClick={(e) => {
                  if (e.button === 1) void closeWindow(w.id)
                }}
              >
                <Icon name={w.minimized ? "minus" : viewById(w.view)?.icon} />
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
            title="Switch between tiling and floating"
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
        <div className="relative isolate min-w-0 flex-1 overflow-hidden bg-muted/40" ref={canvasRef}>
          {state.wins.length === 0 && (
            <Empty className="absolute inset-0">
              <EmptyHeader>
                <EmptyTitle>{manifest.title}</EmptyTitle>
                <EmptyDescription>
                  Open something from the menu, press <Kbd>{showKeys("cmd+k")}</Kbd> for the palette, or ask the agent.
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
          {state.wins.map((w) => {
            const p = placed.get(w.id)
            if (!p) return null
            return (
              <WindowFrame key={w.id} win={w} placed={p} focused={w.id === state.focus} area={area} modules={modules} />
            )
          })}
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

interface FrameProps {
  win: DeskWin
  placed: Placed
  focused: boolean
  area: Rect
  modules: Record<string, ModuleWeb>
}

/** One window: its frame, title bar and body. Moving and resizing are drawn here while the
 * pointer is down and sent to the server when it lifts. */
function WindowFrame({ win, placed, focused, area, modules }: FrameProps) {
  const [drag, setDrag] = useState<Rect | null>(null)
  const view = viewById(win.view)
  const rect = drag ?? placed

  const start = (e: React.PointerEvent, mode: "move" | "resize") => {
    if (e.button !== 0) return
    if (placed.tiled || win.maximized) return
    e.preventDefault()
    const from = { x: e.clientX, y: e.clientY }
    const base: Rect = { x: placed.x, y: placed.y, w: placed.w, h: placed.h }
    let last = base
    const move = (ev: PointerEvent) => {
      const dx = ev.clientX - from.x
      const dy = ev.clientY - from.y
      last =
        mode === "move"
          ? {
              ...base,
              x: Math.min(Math.max(base.x + dx, 0), Math.max(0, area.w - 80)),
              y: Math.min(Math.max(base.y + dy, 0), Math.max(0, area.h - 40)),
            }
          : { ...base, w: Math.max(MIN_W, base.w + dx), h: Math.max(MIN_H, base.h + dy) }
      setDrag(last)
    }
    const upFn = () => {
      window.removeEventListener("pointermove", move)
      window.removeEventListener("pointerup", upFn)
      if (last !== base) {
        void act({ type: "rect", id: win.id, rect: last }).finally(() => setDrag(null))
      } else {
        setDrag(null)
      }
    }
    window.addEventListener("pointermove", move)
    window.addEventListener("pointerup", upFn)
  }

  const Body = view ? (view.module === "surface" ? null : modules[view.module]?.views[view.render.entry]) : undefined
  const CoreBody = view?.module === "surface" ? modules.surface?.views[view.render.entry] : undefined
  const Cmp = Body ?? CoreBody
  const api = useMemo(() => moduleApi(win.module), [win.module])
  const pub = useMemo(() => toWin(win, focused ? win.id : ""), [win, focused])

  return (
    <section
      className="group absolute flex flex-col overflow-hidden rounded-lg border bg-background shadow-md transition-[left,top,width,height] duration-150 data-[dragging=true]:select-none data-[dragging=true]:transition-none data-[focused=true]:border-ring data-[focused=true]:shadow-xl data-[focused=true]:ring-1 data-[focused=true]:ring-ring"
      data-focused={focused}
      data-tiled={placed.tiled}
      data-dragging={!!drag}
      style={{
        left: rect.x,
        top: rect.y,
        width: rect.w,
        height: rect.h,
        zIndex: placed.z + (focused && !placed.tiled ? 1 : 0),
      }}
      onPointerDownCapture={() => {
        if (!focused) void act({ type: "focus", id: win.id })
      }}
    >
      <header
        className="flex h-8 shrink-0 select-none items-center gap-2 border-b bg-card pr-1 pl-3 text-muted-foreground text-xs group-data-[tiled=false]:cursor-grab group-data-[focused=true]:text-foreground"
        onPointerDown={(e) => start(e, "move")}
        onDoubleClick={() => void act({ type: "maximize", id: win.id })}
      >
        <Icon name={view?.icon} size={13} />
        <span className="min-w-0 flex-1 truncate">{win.dirty ? `● ${win.title}` : win.title}</span>
        <span className="flex" onPointerDown={(e) => e.stopPropagation()}>
          <Button
            variant="ghost"
            size="icon-xs"
            title="Float or re-tile"
            onClick={() => void act({ type: "float", id: win.id })}
          >
            <Icon name={win.floating ? "pin" : "picture-in-picture-2"} />
          </Button>
          <Button
            variant="ghost"
            size="icon-xs"
            title="Hide"
            onClick={() => void act({ type: "minimize", id: win.id })}
          >
            <Icon name="minus" />
          </Button>
          <Button
            variant="ghost"
            size="icon-xs"
            title="Maximize or restore"
            onClick={() => void act({ type: "maximize", id: win.id })}
          >
            <Icon name={win.maximized ? "minimize-2" : "maximize-2"} />
          </Button>
          <Button variant="ghost" size="icon-xs" title="Close" onClick={() => void closeWindow(win.id)}>
            <Icon name="x" />
          </Button>
        </span>
      </header>
      <div
        className="relative min-h-0 flex-1 overflow-auto data-[bare=true]:flex data-[bare=true]:flex-col data-[bare=true]:overflow-hidden"
        data-bare={!!view?.bare}
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
      {!placed.tiled && !win.maximized && (
        <div
          className="absolute right-0 bottom-0 size-4 cursor-nwse-resize"
          onPointerDown={(e) => start(e, "resize")}
        />
      )}
    </section>
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
