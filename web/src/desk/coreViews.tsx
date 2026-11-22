/**
 * The core's own windows: Settings, Layouts, Marks and Cards. They are views like any module's.
 * The Components window (surface/gallery) is separate (../gallery) because it draws every
 * registry component, and an app outside this repository installs only the ones it uses.
 */
import { useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { ActivityTimelineWindow } from "../agent/ActivityTimeline"
import { AgentBindings } from "../agent/AgentBindings"
import { CardsWindow } from "../cards/views"
import { CiteRecordView } from "../cite/record.tsx"
import { Workflows } from "../gallery/workflows"
import { Icon, type Mark, type ModuleWeb, replyMark, resolveMark, useEvent, type ViewProps } from "../sdk"
import { allSettings, call, loadSettings, type SettingEntry, setSetting } from "../sdk/runtime"
import { AlignWorkbench } from "./AlignWorkbench"
import { ContextInspector } from "./ContextInspector"
import {
  MAX_NAV_DEPTH,
  type NavGroupDef,
  type NavItemDef,
  type NavTreeNode,
  saveNavOverride,
  useNavModel,
  useNavTree,
} from "./navGroups"
import { app, deleteLayout, deskState, type LayoutDef, listLayouts, loadLayout, saveLayout } from "./store"
import { TrackersWorkbench } from "./TrackersWorkbench"

type SettingsSectionId = "appearance" | "workspace" | "navigation" | "agent" | "connections" | "modules"

const SETTINGS_SECTIONS: { id: SettingsSectionId; label: string; icon: string; subtitle: string }[] = [
  {
    id: "appearance",
    label: "Appearance & Theme",
    icon: "palette",
    subtitle: "Color theme, dark/light polarity, typography, and window spacing.",
  },
  {
    id: "workspace",
    label: "Workspace & Windows",
    icon: "layout-grid",
    subtitle: "Module window behavior, agent rail sizing, and safety prompts.",
  },
  {
    id: "navigation",
    label: "Navigation & Menu",
    icon: "folder-tree",
    subtitle: "Organize menu groups up to 3 levels deep, reorder modules, and customize navigation.",
  },
  {
    id: "agent",
    label: "Agent & Context",
    icon: "bot",
    subtitle: "Context selector mode, card placement, wizard pagination, and developer inspector.",
  },
  {
    id: "connections",
    label: "Connections & Tether",
    icon: "cable",
    subtitle: "Shared Tether MCP transport, hub endpoints, and workspace integrations.",
  },
  {
    id: "modules",
    label: "Module Preferences",
    icon: "boxes",
    subtitle: "Per-module defaults for Files, Notes, Terminal, Work, Registry, and Operations.",
  },
]

const APPEARANCE_KEYS = new Set(["surface.palette", "surface.mode", "surface.typography", "surface.gap"])
const WORKSPACE_KEYS = new Set(["surface.agent_width", "surface.confirm_close"])
const AGENT_KEYS = new Set([
  "surface.context_mode",
  "surface.context_trace",
  "surface.cards_target",
  "surface.cards_slots",
  "surface.wizard_page_size",
  "surface.dev_mode",
])

function isConnectionKey(e: SettingEntry): boolean {
  return (
    e.key.includes("tether") ||
    e.key.includes("caster") ||
    e.key.includes("tesseract") ||
    e.key.includes("torque") ||
    e.key.includes("token") ||
    e.key.includes("url") ||
    e.key.includes("command") ||
    e.key.includes("args")
  )
}

function SettingControl({
  entry,
  onChange,
  onReset,
}: {
  entry: SettingEntry
  onChange: (entry: SettingEntry, value: unknown) => void
  onReset: (entry: SettingEntry) => void
}) {
  const isShortEnum = entry.type === "enum" && (entry.options?.length ?? 0) > 0 && (entry.options?.length ?? 0) <= 3
  return (
    <div className="flex items-center justify-between gap-4 border-b border-border/50 py-2.5 last:border-b-0">
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex items-center gap-2">
          <span className="font-medium text-foreground text-xs">{entry.title}</span>
          {entry.set && (
            <Badge variant="outline" className="h-4 px-1 font-mono text-[9px] text-primary">
              modified
            </Badge>
          )}
        </div>
        {entry.about && <span className="text-[11px] text-muted-foreground leading-snug">{entry.about}</span>}
        <code className="font-mono text-[10px] text-muted-foreground/75">{entry.full_key}</code>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {entry.type === "bool" && (
          <Switch checked={!!entry.value} onCheckedChange={(on) => onChange(entry, on)} aria-label={entry.title} />
        )}
        {isShortEnum && (
          <div className="inline-flex items-center rounded-sm border border-border/80 bg-muted/40 p-0.5">
            {entry.options?.map((o) => {
              const active = String(entry.value) === o
              return (
                <Button
                  key={o}
                  type="button"
                  variant={active ? "secondary" : "ghost"}
                  size="xs"
                  aria-label={entry.title}
                  className="h-6 rounded-sm px-2 text-[11px] capitalize"
                  onClick={() => onChange(entry, o)}
                >
                  {o}
                </Button>
              )
            })}
          </div>
        )}
        {entry.type === "enum" && !isShortEnum && (
          <Select value={String(entry.value)} onValueChange={(v) => onChange(entry, v)}>
            <SelectTrigger size="sm" aria-label={entry.title} className="h-7 min-w-36 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {entry.options?.map((o) => (
                <SelectItem key={o} value={o}>
                  {o}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {entry.type === "int" && (
          <Input
            className="h-7 w-20 text-right font-mono text-xs"
            type="number"
            aria-label={entry.title}
            value={Number(entry.value)}
            onChange={(ev) => {
              const n = Number.parseInt(ev.target.value, 10)
              if (Number.isFinite(n)) onChange(entry, n)
            }}
          />
        )}
        {entry.type === "string" && (
          <Input
            className="h-7 w-60 font-mono text-xs"
            aria-label={entry.title}
            defaultValue={String(entry.value ?? "")}
            key={String(entry.value)}
            onBlur={(ev) => {
              if (ev.target.value !== entry.value) onChange(entry, ev.target.value)
            }}
          />
        )}
        {entry.set && (
          <Button
            variant="ghost"
            size="xs"
            className="h-6 px-1.5 text-[11px] text-muted-foreground hover:text-foreground"
            title="Reset to default"
            onClick={() => onReset(entry)}
          >
            reset
          </Button>
        )}
      </div>
    </div>
  )
}

function NavGroupConfigEditor() {
  const menuViews = useMemo(() => app().views.filter((v) => v.menu !== false), [])
  const tree = useNavTree(menuViews, true)
  const model = useNavModel(menuViews)
  const [newTitle, setNewTitle] = useState("")
  const [newIcon, setNewIcon] = useState("folder")
  const [newParent, setNewParent] = useState<string>("__root__")

  const flatGroups = useMemo(() => {
    const out: { group: NavTreeNode; depth: number }[] = []
    const walk = (list: NavTreeNode[], depth: number) => {
      for (const g of list) {
        out.push({ group: g, depth })
        walk(g.subgroups, depth + 1)
      }
    }
    walk(tree, 1)
    return out
  }, [tree])

  const commitModel = (nextGroups: NavGroupDef[], nextItems: NavItemDef[]) => {
    saveNavOverride({
      groups: nextGroups,
      items: nextItems.map((i) => ({
        id: i.id,
        groupId: i.groupId,
        order: i.order,
        hidden: i.hidden,
        title: i.title,
      })),
    })
  }

  const addGroup = (e: React.FormEvent) => {
    e.preventDefault()
    const title = newTitle.trim()
    if (!title) return
    const id = `custom.${title
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "")}-${Date.now().toString(36).slice(-3)}`
    const nextGroup: NavGroupDef = {
      id,
      title,
      icon: newIcon.trim() || "folder",
      parentId: newParent === "__root__" ? undefined : newParent,
      order: model.groups.length * 10 + 100,
    }
    commitModel([...model.groups, nextGroup], model.items)
    setNewTitle("")
  }

  const moveItemToGroup = (itemId: string, groupId: string) => {
    commitModel(
      model.groups,
      model.items.map((i) => (i.id === itemId ? { ...i, groupId } : i)),
    )
  }

  const nudgeItemOrder = (itemId: string, currentOrder: number, delta: number) => {
    commitModel(
      model.groups,
      model.items.map((i) => (i.id === itemId ? { ...i, order: currentOrder + delta } : i)),
    )
  }

  const toggleItemHidden = (itemId: string, hidden: boolean) => {
    commitModel(
      model.groups,
      model.items.map((i) => (i.id === itemId ? { ...i, hidden } : i)),
    )
  }

  const nudgeGroupOrder = (group: NavTreeNode, delta: number) => {
    commitModel(
      model.groups.map((g) => (g.id === group.id ? { ...g, order: group.order + delta } : g)),
      model.items,
    )
  }

  const changeGroupParent = (group: NavTreeNode, parentId: string) => {
    commitModel(
      model.groups.map((g) =>
        g.id === group.id ? { ...g, parentId: parentId === "__root__" ? undefined : parentId } : g,
      ),
      model.items,
    )
  }

  const resetMenu = () => {
    saveNavOverride(null)
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/80 bg-card/60 p-3">
        <div className="flex flex-col gap-0.5">
          <div className="flex items-center gap-2">
            <span className="font-semibold text-foreground text-xs">Menu Hierarchy & Nav Groups</span>
            <Badge variant="secondary" className="text-[10px]">
              Up to {MAX_NAV_DEPTH} levels deep
            </Badge>
          </div>
          <span className="text-[11px] text-muted-foreground">
            Modules and plugins can register nav groups or attach items via{" "}
            <code className="font-mono text-[10px]">registerNavGroup</code> and{" "}
            <code className="font-mono text-[10px]">registerNavItem</code>. Reorganize groups and order below.
          </span>
        </div>
        <Button variant="outline" size="xs" onClick={resetMenu}>
          <Icon name="rotate-ccw" size={11} />
          Reset Menu Defaults
        </Button>
      </div>

      <form
        onSubmit={addGroup}
        className="flex flex-wrap items-end gap-2 rounded-md border border-border/70 bg-muted/20 p-3"
      >
        <div className="flex min-w-44 flex-1 flex-col gap-1">
          <span className="font-medium text-[10px] text-muted-foreground uppercase tracking-wider">
            New Nav Group Title
          </span>
          <Input
            className="h-7 text-xs"
            placeholder="e.g. Fleet Operations"
            value={newTitle}
            onChange={(e) => setNewTitle(e.target.value)}
          />
        </div>
        <div className="flex w-32 flex-col gap-1">
          <span className="font-medium text-[10px] text-muted-foreground uppercase tracking-wider">Icon</span>
          <Input
            className="h-7 font-mono text-xs"
            placeholder="folder"
            value={newIcon}
            onChange={(e) => setNewIcon(e.target.value)}
          />
        </div>
        <div className="flex w-48 flex-col gap-1">
          <span className="font-medium text-[10px] text-muted-foreground uppercase tracking-wider">
            Parent Group (Max L{MAX_NAV_DEPTH})
          </span>
          <Select value={newParent} onValueChange={(v) => setNewParent(String(v))}>
            <SelectTrigger size="sm" className="h-7 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__root__">Top Level (L1)</SelectItem>
              {flatGroups
                .filter((g) => g.depth < MAX_NAV_DEPTH)
                .map(({ group, depth }) => (
                  <SelectItem key={group.id} value={group.id}>
                    {"— ".repeat(depth)}
                    {group.title} (L{depth + 1})
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
        </div>
        <Button type="submit" size="sm" className="h-7 text-xs" disabled={!newTitle.trim()}>
          <Icon name="plus" size={12} />
          Add Group
        </Button>
      </form>

      <div className="flex flex-col gap-2.5">
        {flatGroups.map(({ group, depth }) => (
          <div
            key={group.id}
            className="flex flex-col gap-2 rounded-md border border-border/80 bg-card/70 p-2.5"
            style={{ marginLeft: `${(depth - 1) * 16}px` }}
          >
            <div className="flex flex-wrap items-center gap-2 border-b border-border/50 pb-2">
              <Icon name={group.icon || "folder"} size={13} className="text-primary" />
              <span className="font-semibold text-foreground text-xs">{group.title}</span>
              <Badge variant="outline" className="h-4 px-1 font-mono text-[9px]">
                L{depth} · #{group.order}
              </Badge>
              <code className="font-mono text-[10px] text-muted-foreground">{group.id}</code>
              <span className="ml-auto flex items-center gap-1">
                <Select value={group.parentId ?? "__root__"} onValueChange={(v) => changeGroupParent(group, String(v))}>
                  <SelectTrigger size="sm" className="h-6 w-36 text-[11px]" aria-label={`Parent of ${group.title}`}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__root__">Top Level (L1)</SelectItem>
                    {flatGroups
                      .filter((cand) => cand.group.id !== group.id && cand.depth < MAX_NAV_DEPTH)
                      .map((cand) => (
                        <SelectItem key={cand.group.id} value={cand.group.id}>
                          {"— ".repeat(cand.depth)}
                          {cand.group.title}
                        </SelectItem>
                      ))}
                  </SelectContent>
                </Select>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  title="Move group up"
                  onClick={() => nudgeGroupOrder(group, -15)}
                >
                  <Icon name="arrow-up" size={11} />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  title="Move group down"
                  onClick={() => nudgeGroupOrder(group, 15)}
                >
                  <Icon name="arrow-down" size={11} />
                </Button>
              </span>
            </div>

            {group.items.length === 0 ? (
              <p className="px-1 text-[11px] text-muted-foreground italic">No direct items in this group.</p>
            ) : (
              <div className="flex flex-col gap-1">
                {group.items.map((item) => {
                  const hidden = item.hidden ?? false
                  return (
                    <div
                      key={item.id}
                      className="flex items-center gap-2 rounded-sm bg-muted/25 px-2 py-1 text-xs hover:bg-muted/45"
                    >
                      <Icon name={item.icon || "app-window"} size={12} className="text-muted-foreground" />
                      <span className={hidden ? "text-muted-foreground line-through" : "font-medium text-foreground"}>
                        {item.title}
                      </span>
                      <code className="font-mono text-[10px] text-muted-foreground">{item.viewId || item.id}</code>
                      <span className="ml-auto flex items-center gap-1">
                        <Select value={item.groupId} onValueChange={(g) => moveItemToGroup(item.id, String(g))}>
                          <SelectTrigger
                            size="sm"
                            className="h-6 w-36 text-[11px]"
                            aria-label={`Group for ${item.title}`}
                          >
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            {flatGroups.map((g) => (
                              <SelectItem key={g.group.id} value={g.group.id}>
                                {"— ".repeat(g.depth - 1)}
                                {g.group.title}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          title="Move item up"
                          onClick={() => nudgeItemOrder(item.id, item.order, -15)}
                        >
                          <Icon name="arrow-up" size={11} />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          title="Move item down"
                          onClick={() => nudgeItemOrder(item.id, item.order, 15)}
                        >
                          <Icon name="arrow-down" size={11} />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          title={hidden ? "Show in menu" : "Hide from menu"}
                          onClick={() => toggleItemHidden(item.id, !hidden)}
                        >
                          <Icon name={hidden ? "eye-off" : "eye"} size={11} />
                        </Button>
                      </span>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

function Settings({ win, desk }: ViewProps) {
  const [entries, setEntries] = useState<SettingEntry[]>(allSettings())
  const [error, setError] = useState("")
  const [search, setSearch] = useState("")
  const requestedSection = typeof win?.args?.section === "string" ? (win.args.section as SettingsSectionId) : undefined
  const [section, setSection] = useState<SettingsSectionId>(requestedSection ?? "appearance")

  useEffect(() => {
    if (requestedSection && SETTINGS_SECTIONS.some((s) => s.id === requestedSection)) {
      setSection(requestedSection)
    }
  }, [requestedSection])

  const refresh = useCallback(() => void loadSettings().then(setEntries), [])
  useEffect(refresh, [refresh])
  // A change made anywhere (another window, the agent, the CLI) shows here at once.
  useEvent("config.changed", refresh)

  const change = (e: SettingEntry, value: unknown) => {
    setError("")
    setSetting(e.full_key, value).catch((err: Error) => setError(`${e.title}: ${err.message}`))
  }
  const reset = (e: SettingEntry) =>
    void call("DELETE", `/api/settings/${encodeURIComponent(e.full_key)}`).then(refresh)

  const visibleEntries = useMemo(
    () => entries.filter((e) => e.full_key !== "surface.nav_config" && e.full_key !== "agent.bindings"),
    [entries],
  )

  const q = search.trim().toLowerCase()
  const filteredEntries = useMemo(() => {
    if (!q) return visibleEntries
    return visibleEntries.filter(
      (e) =>
        e.title.toLowerCase().includes(q) ||
        e.full_key.toLowerCase().includes(q) ||
        (e.about?.toLowerCase().includes(q) ?? false) ||
        e.scope.toLowerCase().includes(q),
    )
  }, [visibleEntries, q])

  const scopeTitle = (scope: string) =>
    scope === "surface" ? "Desk" : (app().modules.find((m) => m.id === scope)?.title ?? scope)

  const appearanceEntries = visibleEntries.filter((e) => APPEARANCE_KEYS.has(e.full_key))
  const workspaceEntries = visibleEntries.filter((e) => WORKSPACE_KEYS.has(e.full_key))
  const agentEntries = visibleEntries.filter((e) => AGENT_KEYS.has(e.full_key))
  const connectionEntries = visibleEntries.filter((e) => e.scope !== "surface" && isConnectionKey(e))
  const moduleEntries = visibleEntries.filter(
    (e) =>
      !APPEARANCE_KEYS.has(e.full_key) &&
      !WORKSPACE_KEYS.has(e.full_key) &&
      !AGENT_KEYS.has(e.full_key) &&
      !(e.scope !== "surface" && isConnectionKey(e)),
  )

  const moduleScopes = [...new Set(moduleEntries.map((e) => e.scope))]
  const connectionScopes = [...new Set(connectionEntries.map((e) => e.scope))]
  const activeMeta = SETTINGS_SECTIONS.find((s) => s.id === section) ?? SETTINGS_SECTIONS[0]

  return (
    <div className="flex h-full min-h-0 bg-background text-xs">
      {/* Left Navigation Sidebar */}
      <aside className="flex w-52 shrink-0 flex-col gap-2 border-r border-border bg-card/40 p-2.5">
        <div className="px-1 pt-0.5">
          <div className="font-semibold text-foreground text-xs">Settings Workbench</div>
          <p className="text-[11px] text-muted-foreground">Live database config — zero restart.</p>
        </div>
        <div className="relative">
          <Input
            className="h-7 pl-2 text-xs"
            placeholder="Filter settings…"
            aria-label="Filter settings"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          {search && (
            <Button
              variant="ghost"
              size="icon-xs"
              className="absolute top-1 right-1 h-5 w-5"
              onClick={() => setSearch("")}
              aria-label="Clear filter"
            >
              <Icon name="x" size={10} />
            </Button>
          )}
        </div>
        <nav aria-label="Settings categories" className="flex flex-col gap-0.5">
          {SETTINGS_SECTIONS.map((s) => {
            const active = !q && section === s.id
            return (
              <Button
                key={s.id}
                variant={active ? "secondary" : "ghost"}
                size="sm"
                className="h-7 justify-start gap-2 px-2 text-xs"
                onClick={() => {
                  setSearch("")
                  setSection(s.id)
                }}
              >
                <Icon name={s.icon} size={13} className={active ? "text-primary" : "text-muted-foreground"} />
                <span className="truncate">{s.label}</span>
              </Button>
            )
          })}
        </nav>
        <div className="mt-auto rounded-md border border-border/60 bg-muted/20 p-2 text-[11px] text-muted-foreground">
          <div className="flex items-center justify-between">
            <span>Total settings</span>
            <span className="font-mono text-foreground">{visibleEntries.length}</span>
          </div>
          <div className="flex items-center justify-between">
            <span>Custom overrides</span>
            <span className="font-mono text-primary">{visibleEntries.filter((e) => e.set).length}</span>
          </div>
        </div>
      </aside>

      {/* Main Content Area */}
      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        <div className="mx-auto flex max-w-2xl flex-col gap-4">
          {error && (
            <div className="rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-destructive text-xs">
              {error}
            </div>
          )}

          {q ? (
            <section className="flex flex-col gap-2">
              <header className="border-b border-border pb-2">
                <h2 className="font-semibold text-foreground text-sm">
                  Search Results for “{search}” ({filteredEntries.length})
                </h2>
              </header>
              {filteredEntries.length === 0 ? (
                <p className="py-6 text-center text-muted-foreground text-xs">No matching settings.</p>
              ) : (
                <div className="rounded-md border border-border/80 bg-card/60 px-3 py-1">
                  {filteredEntries.map((e) => (
                    <SettingControl key={e.full_key} entry={e} onChange={change} onReset={reset} />
                  ))}
                </div>
              )}
            </section>
          ) : (
            <>
              <header className="flex items-start justify-between gap-3 border-b border-border pb-2.5">
                <div className="flex flex-col gap-0.5">
                  <div className="flex items-center gap-2">
                    <Icon name={activeMeta.icon} size={15} className="text-primary" />
                    <h2 className="font-semibold text-foreground text-sm">{activeMeta.label}</h2>
                  </div>
                  <p className="text-muted-foreground text-xs">{activeMeta.subtitle}</p>
                </div>
                {section === "workspace" && (
                  <Button variant="outline" size="xs" onClick={() => void desk.open("surface.layouts")}>
                    <Icon name="layout-dashboard" size={11} />
                    Manage Saved Layouts
                  </Button>
                )}
              </header>

              {section === "appearance" && (
                <div className="rounded-md border border-border/80 bg-card/60 px-3 py-1">
                  {appearanceEntries.map((e) => (
                    <SettingControl key={e.full_key} entry={e} onChange={change} onReset={reset} />
                  ))}
                </div>
              )}

              {section === "workspace" && (
                <div className="flex flex-col gap-3">
                  <div className="rounded-md border border-border/80 bg-card/60 p-3">
                    <div className="flex items-center justify-between">
                      <div className="flex flex-col gap-0.5">
                        <span className="font-medium text-foreground text-xs">Module Window Behavior</span>
                        <span className="text-[11px] text-muted-foreground">
                          Modules open full-frame by default and replace the previous module when switched. Closing a
                          module restores the previous module or trail. Autotiling happens within modules that opt into
                          multi-pane tiling (such as Terminal).
                        </span>
                      </div>
                      <Badge variant="secondary" className="shrink-0 font-mono text-[10px]">
                        Full by default
                      </Badge>
                    </div>
                  </div>
                  <div className="rounded-md border border-border/80 bg-card/60 px-3 py-1">
                    {workspaceEntries.map((e) => (
                      <SettingControl key={e.full_key} entry={e} onChange={change} onReset={reset} />
                    ))}
                  </div>
                </div>
              )}

              {section === "navigation" && <NavGroupConfigEditor />}

              {section === "agent" && (
                <div className="flex flex-col gap-3">
                  <section className="rounded-md border border-border/80 bg-card/60 p-3">
                    <div className="mb-2 border-b border-border/60 pb-1.5">
                      <h3 className="font-semibold text-foreground text-xs">Module Agents</h3>
                    </div>
                    <AgentBindings />
                  </section>
                  <div className="rounded-md border border-border/80 bg-card/60 px-3 py-1">
                    {agentEntries.map((e) => (
                      <SettingControl key={e.full_key} entry={e} onChange={change} onReset={reset} />
                    ))}
                  </div>
                </div>
              )}

              {section === "connections" && (
                <div className="flex flex-col gap-3">
                  {connectionScopes.map((scope) => (
                    <section key={scope} className="rounded-md border border-border/80 bg-card/60 p-3">
                      <div className="mb-1.5 flex items-center justify-between border-b border-border/60 pb-1.5">
                        <h3 className="font-semibold text-foreground text-xs">{scopeTitle(scope)}</h3>
                        <code className="font-mono text-[10px] text-muted-foreground">{scope}</code>
                      </div>
                      {connectionEntries
                        .filter((e) => e.scope === scope)
                        .map((e) => (
                          <SettingControl key={e.full_key} entry={e} onChange={change} onReset={reset} />
                        ))}
                    </section>
                  ))}
                </div>
              )}

              {section === "modules" && (
                <div className="flex flex-col gap-3">
                  {moduleScopes.map((scope) => (
                    <section key={scope} className="rounded-md border border-border/80 bg-card/60 p-3">
                      <div className="mb-1.5 flex items-center justify-between border-b border-border/60 pb-1.5">
                        <h3 className="font-semibold text-foreground text-xs">{scopeTitle(scope)}</h3>
                        <code className="font-mono text-[10px] text-muted-foreground">{scope}</code>
                      </div>
                      {moduleEntries
                        .filter((e) => e.scope === scope)
                        .map((e) => (
                          <SettingControl key={e.full_key} entry={e} onChange={change} onReset={reset} />
                        ))}
                    </section>
                  ))}
                </div>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  )
}

function Layouts(_: ViewProps) {
  const [layouts, setLayouts] = useState<LayoutDef[]>([])
  const [name, setName] = useState("")
  const [error, setError] = useState("")
  const refresh = useCallback(() => void listLayouts().then(setLayouts), [])
  useEffect(refresh, [refresh])
  useEvent("desk.layouts", refresh)
  const active = deskState().active

  const save = (n: string) => {
    setError("")
    saveLayout(n)
      .then(() => setName(""))
      .catch((err: Error) => setError(err.message))
  }
  return (
    <div className="flex flex-col gap-3 p-4">
      <p className="text-muted-foreground">
        A layout is an arrangement you can come back to: which windows are open, how they sit, and the setup that goes
        with them (your mode, the agent's standing instructions, the reflexes that are on).
      </p>
      <form
        className="flex items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (name.trim()) save(name.trim())
        }}
      >
        <Input
          className="flex-1"
          placeholder="Save the desk as it is now, as…"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <Button type="submit" disabled={!name.trim()}>
          Save
        </Button>
      </form>
      {error && <p className="text-destructive">{error}</p>}
      {layouts.length === 0 && <p className="text-muted-foreground text-xs">No saved layouts yet.</p>}
      {layouts.map((l) => (
        <div
          key={l.name}
          className="flex flex-col gap-2 rounded-lg border bg-card p-3 data-[on=true]:border-ring"
          data-on={l.name === active}
        >
          <div className="flex items-center gap-2">
            <Icon name="layout-dashboard" size={15} />
            <strong>{l.name}</strong>
            {l.name === active && <Badge variant="secondary">in use</Badge>}
            <span className="flex-1" />
            <Button variant="outline" size="sm" onClick={() => void loadLayout(l.name)}>
              Arrange
            </Button>
            <Button
              variant="outline"
              size="sm"
              title="Replace it with the desk as it is now"
              onClick={() => save(l.name)}
            >
              Update
            </Button>
            <Button variant="destructive" size="sm" onClick={() => void deleteLayout(l.name).then(refresh)}>
              Delete
            </Button>
          </div>
          {l.about && <p className="text-muted-foreground">{l.about}</p>}
          <p className="text-muted-foreground text-xs">
            {l.mode} · {l.windows.map((w) => w.title || w.view).join(", ") || "no windows"}
          </p>
          {l.setup && Object.keys(l.setup).length > 0 && (
            <dl className="grid gap-1 text-xs [&_dd]:text-muted-foreground [&_div]:flex [&_div]:gap-2 [&_dt]:w-24 [&_dt]:shrink-0 [&_dt]:text-muted-foreground">
              {Object.entries(l.setup).map(([k, v]) => (
                <div key={k}>
                  <dt>{k}</dt>
                  <dd>{typeof v === "string" ? v : JSON.stringify(v)}</dd>
                </div>
              ))}
            </dl>
          )}
        </div>
      ))}
    </div>
  )
}

function Marks(_: ViewProps) {
  const [marks, setMarks] = useState<Mark[]>([])
  const refresh = useCallback(() => void call<Mark[]>("GET", "/api/marks").then(setMarks), [])
  useEffect(refresh, [refresh])
  useEvent("marks.changed", refresh)
  return (
    <div className="flex flex-col gap-3 p-4">
      <p className="text-muted-foreground">
        Marks are notes pinned to a part of something, by you or the agent. These are the open ones, across every
        module.
      </p>
      {marks.length === 0 && <p className="text-muted-foreground text-xs">No open marks.</p>}
      {marks.map((m) => (
        <MarkCard key={m.id} mark={m} />
      ))}
    </div>
  )
}

function MarkCard({ mark }: { mark: Mark }) {
  const [reply, setReply] = useState("")
  return (
    <div
      className="flex flex-col gap-2 rounded-lg border bg-card p-3 data-[author=agent]:border-l-2 data-[author=agent]:border-l-primary"
      data-author={mark.author}
    >
      <div className="flex items-center gap-2">
        <Icon name={mark.author === "agent" ? "bot" : "user"} size={14} />
        <code className="truncate font-mono text-muted-foreground text-xs" title={mark.ref}>
          {mark.ref}
        </code>
        <span className="flex-1" />
        <Button variant="link" size="xs" onClick={() => void resolveMark(mark.id)}>
          resolve
        </Button>
      </div>
      {mark.quote && <blockquote className="border-l-2 pl-2.5 text-muted-foreground italic">{mark.quote}</blockquote>}
      <p>{mark.body}</p>
      {mark.replies?.map((r) => (
        <p key={r.created + r.body} className="flex items-center gap-1.5 pl-2 text-muted-foreground">
          <Icon name={r.author === "agent" ? "bot" : "user"} size={12} /> {r.body}
        </p>
      ))}
      <form
        className="flex items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (reply.trim()) void replyMark(mark.id, reply.trim()).then(() => setReply(""))
        }}
      >
        <Input className="flex-1" placeholder="Reply…" value={reply} onChange={(e) => setReply(e.target.value)} />
      </form>
    </div>
  )
}

export const coreWeb: ModuleWeb = {
  views: {
    "surface/settings": Settings,
    "surface/layouts": Layouts,
    "surface/marks": Marks,
    "surface/cards": CardsWindow,
    "surface/activity": ActivityTimelineWindow,
    "surface/workflows": (props) => <Workflows {...props} />,
    "surface/align": AlignWorkbench,
    "surface/droppad": (props) => <AlignWorkbench {...props} initialMode="droppad" />,
    "surface/trackers": TrackersWorkbench,
    "align/main": AlignWorkbench,
    "trackers/main": TrackersWorkbench,
    "surface/context": ContextInspector,
    "surface/cite": CiteRecordView,
  },
}
