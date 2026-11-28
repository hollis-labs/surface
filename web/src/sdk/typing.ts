/**
 * Whether the owner is typing: a text field has the focus and they pressed a key in it
 * lately, or it holds text they have not sent. Something that would take over the screen (a
 * modal, a queue that opens itself) asks instead while this is true.
 */

const RECENT_MS = 5000
let lastKey = 0

function editable(el: Element | null): el is HTMLElement {
  if (!el || !(el instanceof HTMLElement)) return false
  if (el.isContentEditable) return true
  if (el instanceof HTMLTextAreaElement) return !el.readOnly && !el.disabled
  if (el instanceof HTMLInputElement) {
    const t = el.type
    return !el.readOnly && !el.disabled && (t === "text" || t === "search" || t === "" || t === "url" || t === "email")
  }
  return false
}

if (typeof document !== "undefined") {
  document.addEventListener(
    "keydown",
    (e) => {
      if (editable(e.target as Element)) lastKey = Date.now()
    },
    true,
  )
}

export function isOwnerTyping(): boolean {
  if (typeof document === "undefined") return false
  const el = document.activeElement
  if (!editable(el)) return false
  if (Date.now() - lastKey < RECENT_MS) return true
  const text = el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement ? el.value : el.textContent
  return (text ?? "").trim() !== ""
}
