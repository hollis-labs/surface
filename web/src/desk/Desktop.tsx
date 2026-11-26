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
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Kbd } from "@/components/ui/kbd"
import { type WindowInfo, WindowManager, type WindowTree } from "@/components/ui/window-manager"
import { AdeAttention, AdeGuide } from "../ade/Overlay"
import { AgentPanel } from "../agent"
import { AccountMenu } from "../auth/AccountMenu"
import { setOpenedFromChat } from "../cards/store"
import { Icon, type ModuleWeb, useSetting } from "../sdk"
import { call, getSetting, isConnected, onConnection, setSetting } from "../sdk/runtime"
import { AttentionBadge } from "./AttentionBadge"
import { useAttention } from "./attention"
import { ConfirmHost } from "./Confirm"
import { clientCommands, runClient } from "./commands"
import { useComposing } from "./composing"
import { GhostCursor } from "./GhostCursor"
import { InboxBadge } from "./InboxBadge"
import { matches, parseKeys, showKeys } from "./keys"
import { moduleApi } from "./moduleApi"
import { narrowProjection, useNarrow } from "./narrow"
import { type NavItemDef, type NavTreeNode, useNavTree } from "./navGroups"
import { Palette } from "./Palette"
import {
  act,
  app,
  type CommandDef,
  closeWindow,
  type DeskWin,
  desk,
  notYetKept,
  runCommand,
  setClientRunner,
  toWin,
  useDeskState,
  viewById,
} from "./store"
import { Clock, ThemePicker } from "./ThemePicker"

/**
 * Filter a WindowNode tree to only include window ids in `allowed`.
 * Used so that the active module opens full-frame by default, while autotiling multiple
 * windows within the active module (e.g. Terminal or popped-out module windows).
 */
function filterTreeToIds(node: WindowTree["root"], allowed: Set<string>): WindowTree["root"] | null {
  if (node.type === "stack") {
    const wins = (node.wins ?? []).filter((id) => allowed.has(id))
    if (wins.length === 0) return null
    return {
      ...node,
      wins,
      selected: Math.min(node.selected ?? 0, wins.length - 1),
    }
  }
  const children = (node.children ?? []).flatMap((c) => {
    const f = filterTreeToIds(c, allowed)
    return f ? [f] : []
  })
  if (children.length === 0) return null
  if (children.length === 1 && children[0].type === "stack") {
    return { type: "row", children: [{ ...children[0], weight: 100 }] }
  }
  return { ...node, children }
}

/**
 * Project the server's WindowTree to the active module's workspace:
 * - Modules open full by default, even when other modules are open in the background.
 * - Selecting or opening a different module replaces the active module full-screen.
 * - Within the active module, if multiple windows of that module are open (e.g. Terminal
 *   autotiling or an explicit pop-out in-module window), they tile together within the module.
 * - When the active module's window is closed, focus returns to the previous module (via
 *   server Owned/Trail fallback), which immediately takes the full frame again.
 */
function projectActiveModuleTree(
  tree: WindowTree,
  wins: DeskWin[],
  focus: string,
  layoutMode: "tile" | "float",
): { projectedTree: WindowTree; visibleIds: Set<string>; backgroundIds: string[] } {
  if (layoutMode === "float" || wins.length <= 1) {
    return {
      projectedTree: tree,
      visibleIds: new Set(wins.map((w) => w.id)),
      backgroundIds: [],
    }
  }
  const focusedWin = wins.find((w) => w.id === focus) ?? wins[wins.length - 1]
  if (!focusedWin) {
    return { projectedTree: tree, visibleIds: new Set(), backgroundIds: [] }
  }

  // Floating windows are always visible over the workspace.
  const floatIds = new Set<string>()
  const collectIds = (n: WindowTree["root"]) => {
    if (n.type === "stack") {
      for (const id of n.wins ?? []) floatIds.add(id)
    } else {
      for (const c of n.children ?? []) collectIds(c)
    }
  }
  for (const f of tree.floats ?? []) collectIds(f.root)

  // Determine which tiled windows belong to the active module.
  // Core utility views ("surface.*") are treated individually by view id so Settings doesn't
  // tile alongside Workflows or Activity unless floated.
  const activeGroupKey = focusedWin.module === "surface" ? focusedWin.view : focusedWin.module
  const groupKeyOf = (w: DeskWin) => (w.module === "surface" ? w.view : w.module)

  const viewDef = viewById(focusedWin.view)
  const moduleTiling = viewDef?.tiling ?? "full"

  const sameModuleWins = wins.filter((w) => !floatIds.has(w.id) && groupKeyOf(w) === activeGroupKey)
  // If the module's tiling behavior is "tile" (e.g. Terminal) or the user explicitly opened multiple
  // windows within this same module, tile them together; otherwise show the focused window full-frame.
  const allowedTiled =
    moduleTiling === "tile" || sameModuleWins.length > 1
      ? new Set(sameModuleWins.map((w) => w.id))
      : new Set([focusedWin.id])

  const filteredRoot = filterTreeToIds(tree.root, allowedTiled) ?? {
    type: "row" as const,
    children:
      allowedTiled.size > 0
        ? [{ type: "stack" as const, id: "stack-active", wins: [...allowedTiled], selected: 0 }]
        : [],
  }
  const root: WindowTree["root"] =
    filteredRoot.type === "row" ? filteredRoot : { type: "row", children: [filteredRoot] }

  const visibleIds = new Set([...allowedTiled, ...floatIds])
  const backgroundIds = wins.filter((w) => !visibleIds.has(w.id)).map((w) => w.id)

  return {
    projectedTree: tree.floats?.length ? { root, floats: tree.floats } : { root },
    visibleIds,
    backgroundIds,
  }
}

/** Recursive 3-level navigation menu renderer. */
function NavMenuBranch({ nodes, onSelectItem }: { nodes: NavTreeNode[]; onSelectItem: (item: NavItemDef) => void }) {
  return (
    <>
      {nodes.map((group) => (
        <DropdownMenuSub key={group.id}>
          <DropdownMenuSubTrigger>
            <Icon name={group.icon ?? "folder"} size={15} />
            <span>{group.title}</span>
          </DropdownMenuSubTrigger>
          <DropdownMenuSubContent className="min-w-56">
            {group.items.map((item) => (
              <DropdownMenuItem key={item.id} onClick={() => onSelectItem(item)}>
                <Icon name={item.icon ?? "app-window"} size={15} />
                <span>{item.title}</span>
              </DropdownMenuItem>
            ))}
            {group.items.length > 0 && group.subgroups.length > 0 && <DropdownMenuSeparator />}
            {group.subgroups.length > 0 && <NavMenuBranch nodes={group.subgroups} onSelectItem={onSelectItem} />}
          </DropdownMenuSubContent>
        </DropdownMenuSub>
      ))}
    </>
  )
}

export interface DesktopProps {
  /** The views compiled into this app's page, by module id. */
  modules: Record<string, ModuleWeb>
}

export function Desktop({ modules }: DesktopProps) {
  const state = useDeskState()
  const manifest = app()
  const [palette, setPalette] = useState(false)
  const [menu, setMenu] = useState(false)
  // A phone or a slim window: one window at a time, with a switcher in the bar (narrow.ts).
  const narrow = useNarrow()
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
      // An element that keeps some chords for itself (a Markdown editor's ⌘K) says so in
      // data-keys-own; inside it those chords are its, not the page's.
      const own = (e.target as Element | null)?.closest?.("[data-keys-own]")?.getAttribute("data-keys-own")
      const kept = own ? own.split(/\s+/).flatMap((k) => parseKeys(k) ?? []) : []
      if (kept.some((c) => matches(c, e))) return
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
  const { projectedTree, visibleIds, backgroundIds } = useMemo(
    () =>
      narrow
        ? narrowProjection(state.wins, state.focus)
        : projectActiveModuleTree(state.tree, state.wins, state.focus, state.layout),
    [narrow, state.tree, state.wins, state.focus, state.layout],
  )
  const visibleWindows = useMemo(() => windows.filter((w) => visibleIds.has(w.id)), [windows, visibleIds])

  // The owner dragged, split, tabbed, floated or resized within the active module: re-attach
  // background module windows so the server's state keeps them alive for module switching & trail return.
  const arrange = useCallback(
    (nextTree: WindowTree, focus: string | undefined, grabbed?: string) => {
      // Narrow: the one window shown is not an arrangement to save; only a change of focus counts.
      if (narrow) {
        return focus && focus !== state.focus ? act({ type: "focus", id: focus }) : Promise.resolve(undefined)
      }
      if (backgroundIds.length === 0) {
        return act({ type: "tree", tree: nextTree, focus, id: grabbed })
      }
      const bgStacks = backgroundIds.map((id) => ({
        type: "stack" as const,
        id: `stack-bg-${id}`,
        weight: 20,
        wins: [id],
        selected: 0,
      }))
      const mergedRoot: WindowTree["root"] = {
        type: "row",
        children: [...(nextTree.root.children ?? []), ...bgStacks],
      }
      const fullTree: WindowTree = nextTree.floats?.length
        ? { root: mergedRoot, floats: nextTree.floats }
        : { root: mergedRoot }
      return act({ type: "tree", tree: fullTree, focus, id: grabbed })
    },
    [backgroundIds, narrow, state.focus],
  )
  const menuViews = manifest.views.filter((v) => v.menu)
  const navTree = useNavTree(menuViews)
  const handleNavSelect = useCallback((item: NavItemDef) => {
    if (item.onSelect) {
      item.onSelect()
      return
    }
    if (item.viewId) {
      if (item.viewId === "surface.cards") {
        setOpenedFromChat(false)
        void desk.open(item.viewId, { args: { manual: true } })
        return
      }
      void desk.open(item.viewId)
      return
    }
    if (item.commandId) {
      void runCommand(item.commandId)
    }
  }, [])
  const hasInbox = manifest.views.some((v) => v.id === "inbox" && v.module === "inbox")
  const barBtn =
    "h-5.5 gap-1.5 rounded-sm px-2 font-mono text-xs text-muted-foreground hover:bg-accent hover:text-foreground"

  const bar = (
    <>
      <div className="flex h-full min-w-0 flex-1 items-center gap-1.5">
        <DropdownMenu open={menu} onOpenChange={setMenu}>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="xs"
                className="h-5.5 rounded-sm px-1.5 text-primary hover:bg-accent hover:text-foreground aria-expanded:bg-accent aria-expanded:text-foreground"
                title="Menu"
                aria-label="Menu"
              >
                <Icon name="layout-grid" size={15} />
              </Button>
            }
          />
          <DropdownMenuContent className="min-w-60" sideOffset={8}>
            <NavMenuBranch nodes={navTree} onSelectItem={handleNavSelect} />
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => void desk.open("surface.gallery", { args: { section: "cards" } })}>
              <Icon name="swatch-book" size={15} />
              <span>Cards &amp; Components Preview</span>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => void desk.open("align")}>
              <Icon name="git-compare-arrows" size={15} />
              <span>Visual Alignment &amp; Drop Pad</span>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => void desk.open("trackers")}>
              <Icon name="list-checks" size={15} />
              <span>Native Trackers</span>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => void desk.open("usage")}>
              <Icon name="gauge" size={15} />
              <span>Usage Monitor</span>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => void desk.open("surface.workflows", { args: { tab: "samples" } })}>
              <Icon name="sparkles" size={15} />
              <span>Workflow Golden Samples</span>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => void desk.open("surface.settings", { args: { section: "navigation" } })}>
              <Icon name="folder-tree" size={15} />
              <span>Organize Menu…</span>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => void desk.open("surface.settings")}>
              <Icon name="settings" size={15} />
              <span>Settings</span>
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => void desk.open("surface.layouts")}>
              <Icon name="layout-dashboard" size={15} />
              <span>Layouts</span>
              {state.active && <DropdownMenuShortcut>{state.active}</DropdownMenuShortcut>}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => void runCommand("layout.toggle")}>
              <Icon name={state.layout === "tile" ? "app-window" : "columns-2"} size={15} />
              <span>{state.layout === "tile" ? "Float windows" : "Tile windows"}</span>
              <DropdownMenuShortcut>⌃⌥T</DropdownMenuShortcut>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        {narrow ? (
          <WindowSwitcher />
        ) : (
          <div className="ml-1 flex min-w-0 items-center gap-0.5 overflow-hidden">
            {state.wins.map((w) => {
              const active = w.id === state.focus
              return (
                <Button
                  key={w.id}
                  variant="ghost"
                  size="xs"
                  className={`relative h-5.5 min-w-0 max-w-44 shrink gap-1.5 rounded-sm px-2 font-mono text-xs ${
                    active
                      ? "text-foreground hover:bg-accent"
                      : "text-muted-foreground hover:bg-accent hover:text-foreground"
                  }`}
                  title={w.title}
                  aria-pressed={active}
                  data-task-active={active || undefined}
                  onClick={() => void act({ type: "focus", id: w.id })}
                  onAuxClick={(e) => {
                    if (e.button === 1) void closeWindow(w.id)
                  }}
                >
                  <Icon
                    name={viewById(w.view)?.icon}
                    size={13}
                    className={active ? "text-primary" : "text-muted-foreground"}
                  />
                  <span className="truncate">{w.dirty ? `● ${w.title}` : w.title}</span>
                </Button>
              )
            })}
          </div>
        )}
      </div>

      <div className="flex h-full shrink-0 items-center gap-1.5">
        <Button
          variant="ghost"
          size="xs"
          className={barBtn}
          title={`Command palette (${showKeys("cmd+k")})`}
          aria-label="Command palette"
          onClick={() => setPalette(true)}
        >
          <Icon name="search" size={14} />
        </Button>

        <AttentionBadge ui={ui} />

        {hasInbox && <InboxBadge />}

        {!narrow && state.active && (
          <Button
            variant="ghost"
            size="xs"
            className={barBtn}
            title="The saved layout in use"
            onClick={() => void desk.open("surface.layouts")}
          >
            <Icon name="layout-dashboard" size={14} />
            <span>{state.active}</span>
          </Button>
        )}

        {!narrow && state.mode && (
          <Badge variant="secondary" title="Your mode">
            {state.mode}
          </Badge>
        )}

        {!narrow && (
          <Button
            variant="ghost"
            size="xs"
            className={barBtn}
            title={`${state.layout === "tile" ? "Tiling" : "Floating"} layout (⌃⌥T)`}
            onClick={() => void runCommand("layout.toggle")}
          >
            <Icon name={state.layout === "tile" ? "columns-2" : "app-window"} size={14} />
            <span>{state.layout}</span>
          </Button>
        )}

        <AccountMenu />
        {!narrow && <ThemePicker />}

        <Button
          variant="ghost"
          size="xs"
          className={`h-5.5 rounded-sm px-1.5 ${
            agentOpen
              ? "bg-accent text-primary hover:bg-accent hover:text-foreground"
              : "text-muted-foreground hover:bg-accent hover:text-foreground"
          }`}
          title={`Agent (${showKeys("cmd+j")})`}
          aria-label="Agent"
          aria-pressed={agentOpen}
          data-bar-on={agentOpen || undefined}
          onClick={() => toggleAgent()}
        >
          <Icon name="bot" size={14} />
        </Button>

        {!narrow && <Clock />}

        <span
          className={`mx-1 size-1.5 rounded-full ${up ? "bg-success" : "bg-warning"}`}
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
        side={narrow ? undefined : <PanelComposer />}
        showSide={agentOpen && !narrow}
        sideSize={panelWidth}
        onSideResize={keepPanelWidth}
      >
        {/* Narrow: the agent panel takes the whole canvas while it is open. */}
        {narrow && agentOpen && (
          <div className="absolute inset-0 z-10 flex flex-col bg-background">
            <PanelComposer />
          </div>
        )}
        {/* isolate: the windows' stacking order stays inside the canvas, under the bar and the overlays. */}
        <div
          className="relative isolate size-full overflow-hidden bg-desk"
          style={{ "--window-gap": `${gap}px` } as React.CSSProperties}
        >
          <WindowManager
            tree={projectedTree}
            windows={visibleWindows}
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

/** The narrow desk's window switcher: the window shown, and the others to switch to. */
function WindowSwitcher() {
  const state = useDeskState()
  const shown = state.wins.find((w) => w.id === state.focus) ?? state.wins[state.wins.length - 1]
  if (!shown) return null
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="xs"
            className="ml-1 h-5.5 min-w-0 max-w-56 shrink gap-1.5 rounded-sm px-2 font-mono text-foreground text-xs hover:bg-accent"
            title="Windows"
            aria-label={`Window: ${shown.title}. Switch windows`}
          >
            <Icon name={viewById(shown.view)?.icon} size={13} className="text-primary" />
            <span className="truncate">{shown.dirty ? `● ${shown.title}` : shown.title}</span>
            {state.wins.length > 1 && <span className="text-muted-foreground">{state.wins.length}</span>}
            <Icon name="chevron-down" size={12} className="text-muted-foreground" />
          </Button>
        }
      />
      <DropdownMenuContent className="min-w-56 max-w-[calc(100vw-1rem)]" sideOffset={8} align="start">
        {state.wins.map((w) => (
          <DropdownMenuItem key={w.id} onClick={() => void act({ type: "focus", id: w.id })}>
            <Icon name={viewById(w.view)?.icon} size={15} />
            <span className="truncate">{w.dirty ? `● ${w.title}` : w.title}</span>
            {w.id === shown.id && <Icon name="check" size={14} className="ml-auto" />}
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => void closeWindow(shown.id)}>
          <Icon name="x" size={15} />
          <span>Close {shown.title}</span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
