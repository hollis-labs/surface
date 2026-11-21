/**
 * Reusable Object Actions registry & hooks.
 *
 * Any referenceable object in Surface (a cited record, workstream, task, note, draft, file,
 * design job, registry record, or URL) can expose a standard action menu with built-in actions
 * (copy, download, forward, bookmark, pin, add to context, send to location) plus custom actions
 * registered by modules or plugins via `registerObjectAction`.
 */
import { useSyncExternalStore } from "react"
import { agentSession } from "../agent"
import { desk } from "../desk/store"
import { addMark } from "./marks"
import { call, getSetting, setSetting } from "./runtime"

export interface ObjectRef {
  /** Cite or module kind, e.g. "ops.workstream", "torque.task", "notes.note", "files.file". */
  kind: string
  /** Canonical identifier within the kind (or full ref). */
  id: string
  /** Human-readable title when known. */
  title?: string
  /** Optional subtitle or summary. */
  subtitle?: string
  /** Optional external or raw download URL. */
  url?: string
  /** Optional text/markdown body for download or forwarding. */
  body?: string
  /** Optional file place & path when backed by Files. */
  place?: string
  path?: string
  /** Arbitrary metadata for module callbacks. */
  meta?: Record<string, unknown>
}

export type ObjectActionGroup = "copy" | "context" | "organize" | "send" | "download" | "custom"

export interface ObjectActionContext {
  /** Send a prompt to the embedded agent and open the agent panel. */
  askAgent: (prompt: string) => Promise<void>
  /** Open a desk view. */
  openView: typeof desk.open
  /** Run a desk command. */
  runCommand: typeof desk.run
  /** Show a transient status toast/feedback message in the menu if needed. */
  notify: (message: string) => void
}

export interface ObjectActionDef {
  id: string
  label: string
  icon?: string
  group?: ObjectActionGroup
  shortcut?: string
  /** Lower numbers appear first within their group. Default: 50. */
  order?: number
  /** Submenu items when an action expands into destinations (e.g. "Send to location"). */
  children?: {
    id: string
    label: string
    icon?: string
    when?: (obj: ObjectRef) => boolean
    run: (obj: ObjectRef, ctx: ObjectActionContext) => void | Promise<void>
  }[]
  /** Predicate to control whether the action applies to `obj`. */
  when?: (obj: ObjectRef) => boolean
  /** Callback invoked when the action is triggered. */
  run?: (obj: ObjectRef, ctx: ObjectActionContext) => void | Promise<void>
}

export interface SavedObjectItem {
  ref: string
  kind: string
  id: string
  title: string
  savedAt: string
}

const PINNED_STORAGE_KEY = "surface.objects.pinned"
const BOOKMARKS_STORAGE_KEY = "surface.objects.bookmarks"

const customActions = new Map<string, ObjectActionDef>()
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

/** Format an ObjectRef into its canonical reference string (`kind:id` or raw id if already prefixed). */
export function formatObjectRef(obj: ObjectRef): string {
  if (obj.kind === "url") return obj.url || obj.id
  if (obj.id.includes("://") || obj.id.startsWith(`${obj.kind}:`)) return obj.id
  const prefixMap: Record<string, string> = {
    "ops.workstream": "ops:",
    "torque.task": "",
    "torque.epic": "",
    "torque.plan": "",
    "torque.project": "",
    "torque.sprint": "",
    "registry.record": "registry:",
    "work.item": "work:",
    "notes.note": "notes:",
    "drafts.draft": "drafts:",
    "design.job": "design:",
  }
  const prefix = prefixMap[obj.kind] ?? `${obj.kind}:`
  return `${prefix}${obj.id}`
}

function readList(key: string): SavedObjectItem[] {
  try {
    const fromSetting = getSetting(key) as SavedObjectItem[] | undefined
    if (Array.isArray(fromSetting)) return fromSetting
    const raw = localStorage.getItem(key)
    return raw ? (JSON.parse(raw) as SavedObjectItem[]) : []
  } catch {
    return []
  }
}

function writeList(key: string, items: SavedObjectItem[]) {
  try {
    localStorage.setItem(key, JSON.stringify(items))
    void setSetting(key, items).catch(() => {})
  } catch {
    // Private browsing fallback
  }
  emit()
}

export function isObjectPinned(obj: ObjectRef): boolean {
  const ref = formatObjectRef(obj)
  return readList(PINNED_STORAGE_KEY).some((i) => i.ref === ref)
}

export function isObjectBookmarked(obj: ObjectRef): boolean {
  const ref = formatObjectRef(obj)
  return readList(BOOKMARKS_STORAGE_KEY).some((i) => i.ref === ref)
}

export function toggleObjectPin(obj: ObjectRef): boolean {
  const ref = formatObjectRef(obj)
  const list = readList(PINNED_STORAGE_KEY)
  const exists = list.some((i) => i.ref === ref)
  const next = exists
    ? list.filter((i) => i.ref !== ref)
    : [{ ref, kind: obj.kind, id: obj.id, title: obj.title || obj.id, savedAt: new Date().toISOString() }, ...list]
  writeList(PINNED_STORAGE_KEY, next)
  return !exists
}

export function toggleObjectBookmark(obj: ObjectRef): boolean {
  const ref = formatObjectRef(obj)
  const list = readList(BOOKMARKS_STORAGE_KEY)
  const exists = list.some((i) => i.ref === ref)
  const next = exists
    ? list.filter((i) => i.ref !== ref)
    : [{ ref, kind: obj.kind, id: obj.id, title: obj.title || obj.id, savedAt: new Date().toISOString() }, ...list]
  writeList(BOOKMARKS_STORAGE_KEY, next)
  return !exists
}

export function listPinnedObjects(): SavedObjectItem[] {
  return readList(PINNED_STORAGE_KEY)
}

export function listBookmarkedObjects(): SavedObjectItem[] {
  return readList(BOOKMARKS_STORAGE_KEY)
}

/**
 * Register a custom object action from a module or plugin.
 * Returns an unregister callback.
 */
export function registerObjectAction(action: ObjectActionDef): () => void {
  customActions.set(action.id, action)
  emit()
  return () => {
    if (customActions.delete(action.id)) emit()
  }
}

/** Trigger a browser download for a referenceable object. */
function downloadObject(obj: ObjectRef) {
  if (obj.place && obj.path) {
    const a = document.createElement("a")
    a.href = `/api/m/files/raw?place=${encodeURIComponent(obj.place)}&path=${encodeURIComponent(obj.path)}&download=1`
    a.download = obj.path.split("/").pop() || obj.id
    a.click()
    return
  }
  if (obj.url && /^https?:\/\//i.test(obj.url)) {
    window.open(obj.url, "_blank", "noopener")
    return
  }
  const content =
    obj.body ||
    `# ${obj.title || obj.id}\n\nReference: ${formatObjectRef(obj)}\nKind: ${obj.kind}\nID: ${obj.id}\n${obj.subtitle ? `Summary: ${obj.subtitle}\n` : ""}`
  const blob = new Blob([content], { type: "text/markdown;charset=utf-8" })
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  const safeName =
    (obj.title || obj.id)
      .toLowerCase()
      .replace(/[^a-z0-9._-]+/g, "-")
      .slice(0, 60) || "object"
  a.href = url
  a.download = `${safeName}.md`
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

/** Built-in default actions available on every referenceable object. */
function builtinActions(obj: ObjectRef): ObjectActionDef[] {
  const ref = formatObjectRef(obj)
  const pinned = isObjectPinned(obj)
  const bookmarked = isObjectBookmarked(obj)

  return [
    {
      id: "builtin.copy-ref",
      label: "Copy reference",
      icon: "copy",
      group: "copy",
      order: 10,
      run: async (_, ctx) => {
        await navigator.clipboard?.writeText(ref).catch(() => {})
        ctx.notify(`Copied ${ref}`)
      },
    },
    {
      id: "builtin.copy-markdown",
      label: "Copy as Markdown link",
      icon: "code",
      group: "copy",
      order: 20,
      run: async (_, ctx) => {
        const md = obj.title ? `[${obj.title}](${obj.url || ref})` : `\`${ref}\``
        await navigator.clipboard?.writeText(md).catch(() => {})
        ctx.notify("Copied Markdown link")
      },
    },
    {
      id: "builtin.add-to-context",
      label: "Add to agent context",
      icon: "sparkles",
      group: "context",
      order: 10,
      run: async (_, ctx) => {
        // Dispatch an event that the AgentPanel listens to so it inserts/attaches into the composer without sending immediately,
        // and open the agent panel.
        window.dispatchEvent(
          new CustomEvent("surface:add-to-context", {
            detail: { ref, kind: obj.kind, id: obj.id, title: obj.title || obj.id },
          }),
        )
        void desk.run("agent.toggle", { open: true })
        ctx.notify(`Added ${ref} to agent context`)
      },
    },
    {
      id: "builtin.pin",
      label: pinned ? "Unpin object" : "Pin object",
      icon: "pin",
      group: "organize",
      order: 10,
      run: (_, ctx) => {
        const nowPinned = toggleObjectPin(obj)
        ctx.notify(nowPinned ? `Pinned ${ref}` : `Unpinned ${ref}`)
      },
    },
    {
      id: "builtin.bookmark",
      label: bookmarked ? "Remove bookmark" : "Bookmark",
      icon: "bookmark",
      group: "organize",
      order: 20,
      run: (_, ctx) => {
        const nowBookmarked = toggleObjectBookmark(obj)
        ctx.notify(nowBookmarked ? `Bookmarked ${ref}` : `Removed bookmark`)
      },
    },
    {
      id: "builtin.mark",
      label: "Add note / mark…",
      icon: "message-square-quote",
      group: "organize",
      order: 30,
      run: async (_, ctx) => {
        const body = window.prompt(`Add a note/mark on ${obj.title || ref}:`)
        if (!body?.trim()) return
        await addMark({ ref, body: body.trim() })
        ctx.notify("Mark saved")
      },
    },
    {
      id: "builtin.forward",
      label: "Forward / Share…",
      icon: "forward",
      group: "send",
      order: 10,
      children: [
        {
          id: "forward.agent",
          label: "Send to Agent chat",
          icon: "bot",
          run: async (_, ctx) => {
            await ctx.askAgent(`Look at ${ref}${obj.title ? ` (“${obj.title}”)` : ""}:`)
          },
        },
        {
          id: "forward.tether",
          label: "Forward via Tether mail",
          icon: "mail",
          run: async () => {
            await desk
              .open("tether", {
                args: { compose: true, subject: obj.title || ref, body: `Reference: ${ref}\n\n${obj.subtitle || ""}` },
              })
              .catch(() => desk.run("agent.ask", { text: `Forward ${ref} (${obj.title || ""}) over Tether mail.` }))
          },
        },
        {
          id: "forward.request-status",
          label: "Request status update",
          icon: "radar",
          when: (o) => o.kind.startsWith("ops.") || o.kind.startsWith("torque.") || o.kind.startsWith("work."),
          run: async (_, ctx) => {
            await ctx.askAgent(
              `Request a current status check on ${ref}${obj.title ? ` (“${obj.title}”)` : ""} — summarize active tasks, blockers, and recent progress.`,
            )
          },
        },
      ],
    },
    {
      id: "builtin.send-to",
      label: "Send to location",
      icon: "send",
      group: "send",
      order: 20,
      children: [
        {
          id: "send.scratch",
          label: "Append to Scratchpad",
          icon: "sticky-note",
          run: async (_, ctx) => {
            const snippet = `\n- **${obj.title || ref}** (\`${ref}\`)${obj.subtitle ? ` — ${obj.subtitle}` : ""}\n`
            await call<{ text?: string }>("GET", "/api/m/scratch/pad")
              .then((cur) =>
                call("PUT", "/api/m/scratch/pad", { text: `${(cur.text ?? "").replace(/\s+$/, "")}\n${snippet}` }),
              )
              .catch(() => {})
            void desk.open("scratch")
            ctx.notify("Sent to Scratchpad")
          },
        },
        {
          id: "send.frag",
          label: "Send to Frag Capture",
          icon: "zap",
          run: async (_, ctx) => {
            const line = `${obj.title ? `${obj.title} — ` : ""}${ref}${obj.subtitle ? `\n${obj.subtitle}` : ""}`
            await call("PUT", "/api/m/frag/draft", { content: line, tags: obj.kind.split(".")[0] }).catch(() => {})
            void desk.open("frag")
            ctx.notify("Sent to Frag Capture")
          },
        },
        {
          id: "send.notes",
          label: "Create Note from object",
          icon: "notebook-pen",
          run: async () => {
            const title = obj.title || obj.id
            await desk.run("notes.new", {
              title,
              text: `# ${title}\n\nSource: ${ref}\n\n${obj.body || obj.subtitle || ""}\n`,
            })
          },
        },
        {
          id: "send.drafts",
          label: "Create Draft from object",
          icon: "file-pen",
          run: async () => {
            const title = obj.title || obj.id
            await desk.run("drafts.new", {
              title,
              body: `# ${title}\n\nReference: ${ref}\n\n${obj.body || obj.subtitle || ""}\n`,
            })
          },
        },
      ],
    },
    {
      id: "builtin.download",
      label: "Download",
      icon: "download",
      group: "download",
      order: 10,
      run: (_, ctx) => {
        downloadObject(obj)
        ctx.notify("Downloaded")
      },
    },
  ]
}

/** Resolve all applicable actions (built-in + module-registered) for `obj`, sorted by group and order. */
export function getObjectActions(obj: ObjectRef): ObjectActionDef[] {
  const all = [...builtinActions(obj), ...customActions.values()]
  return all.filter((a) => (a.when ? a.when(obj) : true)).sort((a, b) => (a.order ?? 50) - (b.order ?? 50))
}

/** Reactive hook returning the actions for `obj`, updating whenever actions or pin/bookmark states change. */
export function useObjectActions(obj: ObjectRef): ObjectActionDef[] {
  useSyncExternalStore(subscribe, () => version)
  return getObjectActions(obj)
}

export function createObjectActionContext(notify: (msg: string) => void = () => {}): ObjectActionContext {
  return {
    askAgent: async (prompt: string) => {
      void desk.run("agent.toggle", { open: true })
      await agentSession().send(prompt)
    },
    openView: desk.open,
    runCommand: desk.run,
    notify,
  }
}
