/**
 * The desktop: a bar, the windows, the agent panel beside them, the palette over them.
 * Window state comes from the server (./store); this file draws it and turns what the
 * owner does into actions.
 */
import { Component, type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react"
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
    <div className="sf-desk" data-layout={state.layout}>
      <header className="sf-bar">
        <div className="sf-bar-side">
          <button type="button" className="sf-bar-btn sf-bar-brand" onClick={() => setMenu((m) => !m)} title="Apps">
            <Icon name="layout-grid" size={14} />
            <span>{manifest.title}</span>
          </button>
          {menu && (
            <>
              <div className="sf-scrim" onClick={() => setMenu(false)} />
              <div className="sf-menu" role="menu">
                {menuViews.map((v) => (
                  <button
                    type="button"
                    key={v.id}
                    role="menuitem"
                    className="sf-menu-item"
                    onClick={() => {
                      setMenu(false)
                      void desk.open(v.id)
                    }}
                  >
                    <Icon name={v.icon} size={15} />
                    <span>{v.title}</span>
                    <span className="sf-menu-mod">{v.module === "surface" ? "" : v.module}</span>
                  </button>
                ))}
              </div>
            </>
          )}
          <div className="sf-tabs">
            {state.wins.map((w) => (
              <button
                type="button"
                key={w.id}
                className="sf-tab"
                data-focused={w.id === state.focus}
                data-min={w.minimized}
                title={w.title}
                onClick={() => void act({ type: "focus", id: w.id })}
                onAuxClick={(e) => {
                  if (e.button === 1) void closeWindow(w.id)
                }}
              >
                <Icon name={viewById(w.view)?.icon} size={13} />
                <span>{w.dirty ? `● ${w.title}` : w.title}</span>
              </button>
            ))}
          </div>
        </div>
        <div className="sf-bar-side sf-bar-end">
          {state.active && (
            <button
              type="button"
              className="sf-bar-btn"
              title="The saved layout in use"
              onClick={() => void desk.open("surface.layouts")}
            >
              <Icon name="layout-dashboard" size={13} />
              <span>{state.active}</span>
            </button>
          )}
          {state.mode && (
            <span className="sf-chip" title="Your mode">
              {state.mode}
            </span>
          )}
          <button
            type="button"
            className="sf-bar-btn"
            title="Switch between tiling and floating"
            onClick={() => void runCommand("layout.toggle")}
          >
            <Icon name={state.layout === "tile" ? "columns-2" : "picture-in-picture-2"} size={13} />
            <span>{state.layout}</span>
          </button>
          <button
            type="button"
            className="sf-bar-btn"
            title={`Command palette ${showKeys("cmd+k")}`}
            onClick={() => setPalette(true)}
          >
            <Icon name="search" size={13} />
            <span>{showKeys("cmd+k")}</span>
          </button>
          <button
            type="button"
            className="sf-bar-btn"
            data-on={agentOpen}
            title={`Agent ${showKeys("cmd+j")}`}
            onClick={() => toggleAgent()}
          >
            <Icon name="bot" size={14} />
          </button>
          <span className="sf-live" data-up={up} title={up ? "Connected" : "Reconnecting…"} />
        </div>
      </header>

      <div className="sf-main">
        <div className="sf-canvas" ref={canvasRef}>
          {state.wins.length === 0 && (
            <div className="sf-empty">
              <p>{manifest.title}</p>
              <p className="sf-empty-hint">
                Open something from the menu, press {showKeys("cmd+k")} for the palette, or ask the agent.
              </p>
            </div>
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
          <aside className="sf-panel" style={{ width: panelWidth }}>
            <AgentPanel desk={desk} title="Agent" />
          </aside>
        )}
      </div>

      {palette && <Palette commands={commands} onClose={() => setPalette(false)} />}
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
      className="sf-win"
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
        className="sf-win-title"
        onPointerDown={(e) => start(e, "move")}
        onDoubleClick={() => void act({ type: "maximize", id: win.id })}
      >
        <Icon name={view?.icon} size={13} />
        <span className="sf-win-name">{win.dirty ? `● ${win.title}` : win.title}</span>
        <span className="sf-win-btns" onPointerDown={(e) => e.stopPropagation()}>
          <button type="button" title="Float or re-tile" onClick={() => void act({ type: "float", id: win.id })}>
            <Icon name={win.floating ? "pin" : "picture-in-picture-2"} size={12} />
          </button>
          <button type="button" title="Hide" onClick={() => void act({ type: "minimize", id: win.id })}>
            <Icon name="minus" size={12} />
          </button>
          <button type="button" title="Maximize or restore" onClick={() => void act({ type: "maximize", id: win.id })}>
            <Icon name={win.maximized ? "minimize-2" : "maximize-2"} size={12} />
          </button>
          <button type="button" title="Close" className="sf-win-close" onClick={() => void closeWindow(win.id)}>
            <Icon name="x" size={13} />
          </button>
        </span>
      </header>
      <div className="sf-win-body" data-bare={!!view?.bare}>
        <Boundary key={win.view}>
          {Cmp ? (
            <Cmp win={pub} desk={desk} api={api} />
          ) : (
            <div className="sf-missing">
              This app's page was built without the view “{win.view}”. Rebuild the page with the module's web entry.
            </div>
          )}
        </Boundary>
      </div>
      {!placed.tiled && !win.maximized && <div className="sf-win-grip" onPointerDown={(e) => start(e, "resize")} />}
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
        <div className="sf-missing">
          <p>This window failed to draw.</p>
          <pre>{this.state.error.message}</pre>
          <button type="button" className="sf-btn" onClick={() => this.setState({ error: null })}>
            Try again
          </button>
        </div>
      )
    }
    return this.props.children
  }
}
