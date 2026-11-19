/** The page's own commands: the ones that only make sense in the page. The rest are the
 * server's (the core's and the modules') and arrive in the app manifest. */
import { agentSession } from "../agent"
import { call } from "../sdk/runtime"
import { type AttendItem, firstItem } from "./attention"
import { act, type CommandDef } from "./store"

export interface UI {
  togglePalette(open?: boolean): void
  toggleAgent(open?: boolean): void
  toggleMenu(): void
}

export const clientCommands: CommandDef[] = [
  {
    id: "palette.toggle",
    title: "Command palette",
    section: "System",
    keys: "cmd+k",
    hidden: true,
    module: "surface",
    server: false,
  },
  {
    id: "agent.toggle",
    title: "Show or hide the agent",
    section: "Agent",
    keys: "cmd+j",
    keywords: "assistant panel chat",
    module: "surface",
    server: false,
  },
  { id: "agent.ask", title: "Ask the agent", section: "Agent", hidden: true, module: "surface", server: false },
  { id: "menu.toggle", title: "Apps menu", section: "System", hidden: true, module: "surface", server: false },
  {
    id: "attention.jump",
    title: "Jump to what needs me",
    section: "System",
    keys: "cmd+.",
    keywords: "attention waiting unseen needs answer next",
    module: "surface",
    server: false,
  },
]

/** Brings an item in front: its window, or the Cards window for a card (the agent's own cards sit
 * in its panel). */
export async function jumpTo(item: AttendItem | undefined, ui: UI): Promise<boolean> {
  if (!item) return false
  if (item.window) {
    await act({ type: "focus", id: item.window })
    return true
  }
  if (item.card) {
    if (item.by === "agent") ui.toggleAgent(true)
    else await call("POST", "/api/desk/actions", { type: "open", view: "surface.cards" })
    return true
  }
  return false
}

/** Runs a page command. Returns undefined when the id is not one of the page's. */
export function runClient(id: string, ui: UI, args?: Record<string, unknown>): Promise<unknown> | undefined {
  switch (id) {
    case "palette.toggle":
      ui.togglePalette()
      return Promise.resolve(null)
    case "agent.toggle":
      ui.toggleAgent(typeof args?.open === "boolean" ? args.open : undefined)
      return Promise.resolve(null)
    case "agent.ask": {
      // A view hands the agent a message ({text}); the panel opens to show the answer.
      const text = typeof args?.text === "string" ? args.text.trim() : ""
      if (!text) return Promise.resolve(false)
      ui.toggleAgent(true)
      return agentSession().send(text)
    }
    case "menu.toggle":
      ui.toggleMenu()
      return Promise.resolve(null)
    case "attention.jump":
      return call<{ items: AttendItem[] }>("GET", "/api/attend").then((a) =>
        jumpTo(firstItem({ items: a.items ?? [], counts: {} }), ui),
      )
  }
  return undefined
}
