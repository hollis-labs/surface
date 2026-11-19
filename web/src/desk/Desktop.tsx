/**
 * The desktop: the registry's AppFrame with the bar, the windows in its content, the agent
 * panel in its side region, and the palette and ADE's overlays over them.
 * Window state comes from the server (./store); this file draws it and turns what the
 * owner does into actions.
 */
import { applyPalette, DEFAULT_PALETTE, paletteById } from "@hollis-labs/surface-theme"
import { Component, type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { AppFrame } from "@/components/ui/app-frame"
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
import { AdeAttention, AdeGuide } from "../ade/Overlay"
import { AgentPanel } from "../agent"
import { Icon, type ModuleWeb, useSetting } from "../sdk"
import { call, getSetting, isConnected, onConnection, setSetting } from "../sdk/runtime"
import { AttentionBadge } from "./AttentionBadge"
import { useAttention } from "./attention"
import { ConfirmHost } from "./Confirm"
import { clientCommands, runClient } from "./commands"
import { useComposing } from "./composing"
import { GhostCursor } from "./GhostCursor"
import { matches, parseKeys, showKeys } from "./keys"
import { moduleApi } from "./moduleApi"
import { Palette } from "./Palette"
import {
  act,
  app,
  type CommandDef,
  closeWindow,
  desk,
  notYetKept,
  runCommand,
  setClientRunner,
  toWin,
  useDeskState,
  viewById,
} from "./store"
import { Clock, ThemePicker } from "./ThemePicker"

export interface DesktopProps {
  /** The views compiled into this app's page, by module id. */
  modules: Record<string, ModuleWeb>
}

export function Desktop({ modules }: DesktopProps) {
  const state = useDeskState()
  const manifest = app()
  const [palette, setPalette] = useState(false)
  const [menu, setMenu] = useState(false)
  const [agentOpen, setAgentOpen] = useState(() => {
    // Sidebars start hidden; the agent panel opens when the person opens it, and stays as they left it.
    try {
      return localStorage.getItem("surface.agent.open") === "1"
    } catch {
      return false
    }
  })
  const [up, setUp] = useState(isConnected())
  const gap = (useSetting<number>("surface.gap") ?? 10) as number
  useAnswerSeen(agentOpen)
  const paletteId = (useSetting<string>("surface.palette") ?? DEFAULT_PALETTE.id) as string
  const typeStyle = (useSetting<string>("surface.type") ?? "mixed") === "mono" ? "mono" : "mixed"
  const panelWidth = (useSetting<number>("surface.agent_panel_width") ?? 420) as number

  useEffect(() => {
    setUp(isConnected())
    return onConnection(setUp)
  }, [])

  // The palette and the type style are live settings: changing one redraws the desk at once, with no
  // reload. The palette also says dark or light.
  useEffect(() => {
    applyPalette(paletteById(paletteId) ?? DEFAULT_PALETTE, document.documentElement, typeStyle)
  }, [paletteId, typeStyle])

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
        // A delegate's window the owner has not made theirs: who opened it, and Keep (rule 4).
        badge: notYetKept(w) ? (
          <span
            className="text-muted-foreground"
            title={`Opened by the ${w.opened_by}${w.reason ? `: ${w.reason}` : ""}`}
          >
            <Icon name="bot" size={11} />
          </span>
        ) : undefined,
        actions:
          notYetKept(w) || w.held ? (
            <>
              {w.held && (
                <>
                  <span
                    className="text-warning"
                    title={`The ${w.held.by} wants to change what this shows${w.held.title ? ` (${w.held.title})` : ""}. It waits until you are done here.`}
                  >
                    <Icon name="git-pull-request-arrow" size={12} />
                  </span>
                  <Button
                    variant="ghost"
                    size="xs"
                    title="Apply the waiting change now"
                    onPointerDown={(e) => e.stopPropagation()}
                    onClick={(e) => {
                      e.stopPropagation()
                      void act({ type: "held", id: w.id, do: "apply" })
                    }}
                  >
                    Apply
                  </Button>
                  <Button
                    variant="ghost"
                    size="xs"
                    title="Drop the waiting change"
                    onPointerDown={(e) => e.stopPropagation()}
                    onClick={(e) => {
                      e.stopPropagation()
                      void act({ type: "held", id: w.id, do: "drop" })
                    }}
                  >
                    Drop
                  </Button>
                </>
              )}
              {notYetKept(w) && (
                <Button
                  variant="ghost"
                  size="xs"
                  title="Keep this window: it is yours, and the agent will not close it"
                  onPointerDown={(e) => e.stopPropagation()}
                  onClick={(e) => {
                    e.stopPropagation()
                    void act({ type: "keep", id: w.id })
                  }}
                >
                  Keep
                </Button>
              )}
            </>
          ) : undefined,
      })),
    [state.wins],
  )
  // The owner dragged, split, tabbed, floated or resized: the server keeps the result.
  // The window the owner grabbed in a drag goes with the tree, so it becomes theirs (desk rule 4).
  const arrange = useCallback(
    (tree: WindowTree, focus: string | undefined, grabbed?: string) => act({ type: "tree", tree, focus, id: grabbed }),
    [],
  )
  const menuViews = manifest.views.filter((v) => v.menu)

  const bar = (
    <>
      <div className="flex h-full min-w-0 flex-1 items-center gap-1">
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
        <AttentionBadge ui={ui} />
        <ThemePicker />
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
        <Clock />
        <span
          className={`mx-1.5 size-2 rounded-full ${up ? "bg-success" : "bg-warning"}`}
          title={up ? "Connected" : "Reconnecting…"}
        />
      </div>
    </>
  )

  return (
    <>
      <AppFrame
        className="fixed inset-0 text-sm"
        data-layout={state.layout}
        bar={bar}
        side={<PanelComposer />}
        showSide={agentOpen}
        sideSize={panelWidth}
        onSideResize={keepPanelWidth}
      >
        {/* isolate: the windows' stacking order stays inside the canvas, under the bar and the overlays. */}
        <div
          className="relative isolate size-full overflow-hidden bg-desk"
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
        <AdeGuide />
        <GhostCursor />
      </AppFrame>
      <AdeAttention />
      <Palette open={palette} onOpenChange={setPalette} commands={commands} />
      <ConfirmHost />
    </>
  )
}

/** The agent panel, reporting the owner's unsent input in its composer (rule 7). */
function PanelComposer() {
  const ref = useRef<HTMLDivElement>(null)
  useComposing(ref, "")
  return (
    <div ref={ref} className="contents">
      <AgentPanel desk={desk} title="Agent" />
    </div>
  )
}

/** The agent's answer counts as seen (attention subject "agent") when it waits while the panel is
 * open and the page has the focus: as it finishes in front of the owner, or when they open the
 * panel or come back to the page afterwards. */
function useAnswerSeen(panelOpen: boolean) {
  const a = useAttention()
  const [focused, setFocused] = useState(() => document.hasFocus())
  useEffect(() => {
    const on = () => setFocused(true)
    const off = () => setFocused(false)
    window.addEventListener("focus", on)
    window.addEventListener("blur", off)
    return () => {
      window.removeEventListener("focus", on)
      window.removeEventListener("blur", off)
    }
  }, [])
  const waiting = a.items.find((i) => i.subject === "agent" && i.state === "unseen")
  useEffect(() => {
    if (waiting && panelOpen && focused) void call("POST", "/api/attend/seen", { subject: "agent" }).catch(() => {})
  }, [waiting, panelOpen, focused])
}

/** The agent panel's width is a live setting: a drag is kept once it stops. */
let widthTimer: ReturnType<typeof setTimeout> | undefined
function keepPanelWidth(px: number) {
  clearTimeout(widthTimer)
  widthTimer = setTimeout(() => {
    if (px > 0 && px !== getSetting("surface.agent_panel_width"))
      void setSetting("surface.agent_panel_width", px).catch(() => {})
  }, 400)
}

/** One window's body: the view it shows, handed its window, the desk and its module's API. */
function WindowBody({ id, modules }: { id: string; modules: Record<string, ModuleWeb> }) {
  const state = useDeskState()
  const win = state.wins.find((w) => w.id === id)
  const focused = state.focus === id
  const module = win?.module ?? ""
  const api = useMemo(() => moduleApi(module), [module])
  const pub = useMemo(() => (win ? toWin(win, focused ? win.id : "") : null), [win, focused])
  const body = useRef<HTMLDivElement>(null)
  useComposing(body, id)
  if (!win || !pub) return null
  const view = viewById(win.view)
  const Cmp = view ? modules[view.module]?.views[view.render.entry] : undefined
  return (
    <div
      className="relative size-full min-h-0 overflow-auto data-[bare=true]:flex data-[bare=true]:flex-col data-[bare=true]:overflow-hidden"
      ref={body}
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
