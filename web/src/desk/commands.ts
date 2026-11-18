/** The page's own commands: the ones that only make sense in the page. The rest are the
 * server's (the core's and the modules') and arrive in the app manifest. */
import { agentSession } from "../agent"
import type { CommandDef } from "./store"

export interface UI {
  togglePalette(open?: boolean): void
  toggleAgent(open?: boolean): void
  toggleMenu(): void
}

export const clientCommands: CommandDef[] = [
  { id: "palette.toggle", title: "Command palette", section: "System", keys: "cmd+k", hidden: true, module: "surface", server: false },
  { id: "agent.toggle", title: "Show or hide the agent", section: "Agent", keys: "cmd+j", keywords: "assistant panel chat", module: "surface", server: false },
  { id: "agent.ask", title: "Ask the agent", section: "Agent", hidden: true, module: "surface", server: false },
  { id: "menu.toggle", title: "Apps menu", section: "System", hidden: true, module: "surface", server: false },
]

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
  }
  return undefined
}
