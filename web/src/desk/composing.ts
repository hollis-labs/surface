/**
 * Unsent input (desk rule 7): the page tells the server when the owner has typed something not yet
 * sent, in a window or in the agent panel, so a delegate's change to that window is held instead of
 * replacing it, and an agent's open does not take the focus from the panel. Any text field holding
 * text counts. The report is sent only when it changes, and cleared when the field empties or the
 * window goes.
 */
import { type RefObject, useEffect } from "react"
import { act } from "./store"

const fields =
  'textarea, input:not([type]), input[type="text"], input[type="search"], input[type="email"], input[type="url"], [contenteditable="true"]'

function hasText(root: HTMLElement): boolean {
  for (const el of root.querySelectorAll<HTMLElement>(fields)) {
    const v = el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement ? el.value : el.textContent
    if (v && v.trim() !== "") return true
  }
  return false
}

/** Reports whether the element's fields hold unsent text: for window id, or the panel when id is "". */
export function useComposing(ref: RefObject<HTMLElement | null>, id: string) {
  useEffect(() => {
    const root = ref.current
    if (!root) return
    let last = false
    let timer = 0
    let poll = 0
    const report = (now: boolean) => {
      if (now === last) return
      last = now
      void act({ type: "composing", ...(id ? { id } : {}), composing: now }).catch(() => {})
      // While text waits, look again now and then: a field emptied by the view (a message sent)
      // fires no input event.
      window.clearInterval(poll)
      if (now) poll = window.setInterval(() => report(hasText(root)), 3000)
    }
    const check = () => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => report(hasText(root)), 250)
    }
    const events = ["input", "keyup", "submit", "focusout", "click"] as const
    for (const e of events) root.addEventListener(e, check, true)
    return () => {
      for (const e of events) root.removeEventListener(e, check, true)
      window.clearTimeout(timer)
      window.clearInterval(poll)
      if (last) void act({ type: "composing", ...(id ? { id } : {}), composing: false }).catch(() => {})
    }
  }, [ref, id])
}
