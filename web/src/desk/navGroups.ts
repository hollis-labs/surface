/**
 * Nav Groups semantic & 3-level hierarchical menu registry.
 *
 * Modules and plugins can register navigation groups (`registerNavGroup`) and place views or
 * commands into any group (`registerNavItem`) up to 3 levels deep with explicit `order` weights.
 * User customizations (group hierarchy, ordering, custom groups, hidden items) are persisted in
 * the live setting `surface.nav_config`.
 */
import { useSyncExternalStore } from "react"
import { getSetting, setSetting } from "../sdk/runtime"
import type { ViewDef } from "./store"

export const MAX_NAV_DEPTH = 3
export const NAV_SETTING_KEY = "surface.nav_config"

export interface NavGroupDef {
  id: string
  title: string
  icon?: string
  /** Optional parent group id (max 3 levels deep). */
  parentId?: string
  /** Lower numbers sort earlier. */
  order: number
}

export interface NavItemDef {
  id: string
  title: string
  icon?: string
  /** Group id this item belongs to. */
  groupId: string
  /** Lower numbers sort earlier within the group. */
  order: number
  /** View to open when clicked. */
  viewId?: string
  /** Command to run when clicked (if not a view). */
  commandId?: string
  /** Optional callback for dynamic plugin items. */
  onSelect?: () => void
  hidden?: boolean
}

export interface NavConfigOverride {
  groups: NavGroupDef[]
  items: { id: string; groupId: string; order: number; hidden?: boolean; title?: string }[]
}

export interface NavTreeNode {
  kind: "group"
  id: string
  title: string
  icon?: string
  parentId?: string
  order: number
  depth: number
  subgroups: NavTreeNode[]
  items: NavItemDef[]
}

/** Default 3-level nav group hierarchy for Surface Desk. */
export const DEFAULT_NAV_GROUPS: NavGroupDef[] = [
  // Level 1: Top-level domain groups
  { id: "workspace", title: "Workspace & Planning", icon: "target", order: 10 },
  { id: "knowledge", title: "Knowledge & Authoring", icon: "notebook-pen", order: 20 },
  { id: "operations", title: "Operations & Fleet", icon: "radar", order: 30 },
  { id: "previews", title: "Components & Previews", icon: "swatch-book", order: 35 },
  { id: "system", title: "System & Inspector", icon: "terminal", order: 40 },

  // Level 2 subgroups
  { id: "workspace.delivery", title: "Projects & Triage", icon: "inbox", parentId: "workspace", order: 20 },
  { id: "knowledge.research", title: "Research & Files", icon: "compass", parentId: "knowledge", order: 20 },
  { id: "operations.design", title: "Design & Visuals", icon: "palette", parentId: "operations", order: 20 },
  { id: "system.agent", title: "Agent & Diagnostics", icon: "scan-eye", parentId: "system", order: 20 },

  // Level 3 subgroups
  { id: "system.agent.ui", title: "UI & Golden Samples", icon: "swatch-book", parentId: "system.agent", order: 30 },
]

/** Default placement of module views into the 3-level hierarchy. */
const DEFAULT_VIEW_PLACEMENT: Record<string, { groupId: string; order: number }> = {
  // Workspace & Planning (Level 1)
  work: { groupId: "workspace", order: 10 },
  "work.content": { groupId: "workspace", order: 20 },
  "work.writer": { groupId: "workspace", order: 30 },
  // Workspace -> Projects & Triage (Level 2)
  projects: { groupId: "workspace.delivery", order: 10 },
  inbox: { groupId: "workspace.delivery", order: 20 },

  // Knowledge & Authoring (Level 1)
  notes: { groupId: "knowledge", order: 10 },
  drafts: { groupId: "knowledge", order: 20 },
  scratch: { groupId: "knowledge", order: 30 },
  frag: { groupId: "knowledge", order: 40 },
  // Knowledge -> Research & Files (Level 2)
  files: { groupId: "knowledge.research", order: 10 },
  reader: { groupId: "knowledge.research", order: 20 },
  browser: { groupId: "knowledge.research", order: 30 },

  // Operations & Fleet (Level 1)
  ops: { groupId: "operations", order: 10 },
  tether: { groupId: "operations", order: 20 },
  registry: { groupId: "operations", order: 30 },
  // Operations -> Design & Visuals (Level 2)
  design: { groupId: "operations.design", order: 10 },
  diagram: { groupId: "operations.design", order: 20 },
  viewer: { groupId: "operations.design", order: 30 },

  // Components & Previews (Top-level Level 1 for immediate access)
  "surface.gallery": { groupId: "previews", order: 10 },
  "surface.workflows": { groupId: "previews", order: 20 },
  "surface.cards": { groupId: "previews", order: 30 },

  // System & Inspector (Level 1)
  terminal: { groupId: "system", order: 10 },
  assist: { groupId: "system", order: 20 },
  "surface.settings": { groupId: "system", order: 30 },
  "surface.layouts": { groupId: "system", order: 40 },
  // System -> Agent & Diagnostics (Level 2)
  "surface.activity": { groupId: "system.agent", order: 10 },
  "surface.context": { groupId: "system.agent", order: 20 },
  "surface.marks": { groupId: "system.agent", order: 40 },
}

const moduleGroups = new Map<string, NavGroupDef>()
const moduleItems = new Map<string, NavItemDef>()
let version = 0
const listeners = new Set<() => void>()

function emit() {
  version++
  for (const fn of listeners) fn()
}

function subscribe(fn: () => void) {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

/**
 * Expose a hook/callback for modules to register a nav group (up to 3 levels deep).
 */
export function registerNavGroup(group: Omit<NavGroupDef, "order"> & { order?: number }): () => void {
  moduleGroups.set(group.id, { ...group, order: group.order ?? 50 })
  emit()
  return () => {
    if (moduleGroups.delete(group.id)) emit()
  }
}

/**
 * Expose a hook/callback for modules to add an item to a nav group.
 */
export function registerNavItem(item: Omit<NavItemDef, "order"> & { order?: number }): () => void {
  moduleItems.set(item.id, { ...item, order: item.order ?? 50 })
  emit()
  return () => {
    if (moduleItems.delete(item.id)) emit()
  }
}

export function readNavOverride(): NavConfigOverride | null {
  try {
    const fromSetting = getSetting(NAV_SETTING_KEY) as NavConfigOverride | undefined
    if (fromSetting && Array.isArray(fromSetting.groups)) return fromSetting
    const raw = localStorage.getItem(NAV_SETTING_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as NavConfigOverride
    return Array.isArray(parsed?.groups) ? parsed : null
  } catch {
    return null
  }
}

export function saveNavOverride(config: NavConfigOverride | null) {
  try {
    if (!config) {
      localStorage.removeItem(NAV_SETTING_KEY)
      void setSetting(NAV_SETTING_KEY, null).catch(() => {})
    } else {
      localStorage.setItem(NAV_SETTING_KEY, JSON.stringify(config))
      void setSetting(NAV_SETTING_KEY, config).catch(() => {})
    }
  } catch {
    // Ignore storage errors
  }
  emit()
}

/** Calculate depth of a group (1 = root, 2 = child, 3 = grandchild). */
export function groupDepth(groupId: string | undefined, byId: Map<string, NavGroupDef>): number {
  let d = 0
  let cur = groupId
  const seen = new Set<string>()
  while (cur && !seen.has(cur) && d < 10) {
    seen.add(cur)
    const g = byId.get(cur)
    if (!g) break
    d++
    cur = g.parentId
  }
  return Math.max(1, d)
}

/** Merge default groups, module-registered groups, and user overrides into a flat list of groups and items. */
export function resolveNavModel(menuViews: ViewDef[]): { groups: NavGroupDef[]; items: NavItemDef[] } {
  const override = readNavOverride()

  const baseGroups = new Map<string, NavGroupDef>()
  for (const g of DEFAULT_NAV_GROUPS) baseGroups.set(g.id, { ...g })
  for (const g of moduleGroups.values()) baseGroups.set(g.id, { ...g })
  if (override?.groups?.length) {
    baseGroups.clear()
    for (const g of override.groups) baseGroups.set(g.id, { ...g })
    // Ensure newly added default top-level groups (like "previews") and module groups are always present
    for (const g of DEFAULT_NAV_GROUPS) {
      if (!baseGroups.has(g.id)) baseGroups.set(g.id, { ...g })
    }
    for (const g of moduleGroups.values()) {
      if (!baseGroups.has(g.id)) baseGroups.set(g.id, { ...g })
    }
  }

  // Clamp any group whose parent chain exceeds MAX_NAV_DEPTH (3 levels)
  for (const g of baseGroups.values()) {
    if (g.parentId && !baseGroups.has(g.parentId)) {
      g.parentId = undefined
    }
    if (groupDepth(g.id, baseGroups) > MAX_NAV_DEPTH) {
      const parent = g.parentId ? baseGroups.get(g.parentId) : undefined
      g.parentId = parent?.parentId
    }
  }

  const itemOverrides = new Map((override?.items ?? []).map((i) => [i.id, i]))
  const items: NavItemDef[] = []

  for (const v of menuViews) {
    const def = DEFAULT_VIEW_PLACEMENT[v.id] ?? { groupId: "workspace", order: 90 }
    const ov = itemOverrides.get(v.id)
    const targetGroup = ov?.groupId && baseGroups.has(ov.groupId) ? ov.groupId : def.groupId
    items.push({
      id: v.id,
      title: ov?.title || v.title,
      icon: v.icon,
      groupId: baseGroups.has(targetGroup) ? targetGroup : "workspace",
      order: ov?.order ?? def.order,
      viewId: v.id,
      hidden: ov?.hidden ?? false,
    })
  }

  for (const mi of moduleItems.values()) {
    if (items.some((x) => x.id === mi.id)) continue
    const ov = itemOverrides.get(mi.id)
    const targetGroup = ov?.groupId && baseGroups.has(ov.groupId) ? ov.groupId : mi.groupId
    items.push({
      ...mi,
      groupId: baseGroups.has(targetGroup) ? targetGroup : "workspace",
      order: ov?.order ?? mi.order,
      hidden: ov?.hidden ?? mi.hidden,
    })
  }

  const sortedGroups = [...baseGroups.values()].sort((a, b) => a.order - b.order || a.title.localeCompare(b.title))
  const sortedItems = items.sort((a, b) => a.order - b.order || a.title.localeCompare(b.title))
  return { groups: sortedGroups, items: sortedItems }
}

/** Build the 3-level nav tree from `menuViews`. */
export function buildNavTree(menuViews: ViewDef[], includeHidden = false): NavTreeNode[] {
  const { groups, items } = resolveNavModel(menuViews)
  const nodes = new Map<string, NavTreeNode>()
  const byId = new Map(groups.map((g) => [g.id, g]))

  for (const g of groups) {
    nodes.set(g.id, {
      kind: "group",
      id: g.id,
      title: g.title,
      icon: g.icon,
      parentId: g.parentId,
      order: g.order,
      depth: groupDepth(g.id, byId),
      subgroups: [],
      items: [],
    })
  }

  for (const item of items) {
    if (!includeHidden && item.hidden) continue
    const target = nodes.get(item.groupId) ?? nodes.values().next().value
    if (target) target.items.push(item)
  }

  const roots: NavTreeNode[] = []
  for (const node of nodes.values()) {
    if (node.parentId && nodes.has(node.parentId) && node.depth <= MAX_NAV_DEPTH) {
      nodes.get(node.parentId)?.subgroups.push(node)
    } else {
      roots.push(node)
    }
  }

  const sortNode = (n: NavTreeNode) => {
    n.subgroups.sort((a, b) => a.order - b.order || a.title.localeCompare(b.title))
    n.items.sort((a, b) => a.order - b.order || a.title.localeCompare(b.title))
    for (const sub of n.subgroups) sortNode(sub)
  }
  roots.sort((a, b) => a.order - b.order || a.title.localeCompare(b.title))
  for (const r of roots) sortNode(r)

  if (includeHidden) return roots

  // Prune empty branches in the live menu
  const hasContent = (n: NavTreeNode): boolean => {
    n.subgroups = n.subgroups.filter(hasContent)
    return n.items.length > 0 || n.subgroups.length > 0
  }
  return roots.filter(hasContent)
}

export function useNavTree(menuViews: ViewDef[], includeHidden = false): NavTreeNode[] {
  useSyncExternalStore(subscribe, () => version)
  return buildNavTree(menuViews, includeHidden)
}

export function useNavModel(menuViews: ViewDef[]): { groups: NavGroupDef[]; items: NavItemDef[] } {
  useSyncExternalStore(subscribe, () => version)
  return resolveNavModel(menuViews)
}
