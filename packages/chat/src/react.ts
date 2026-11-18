/**
 * React bindings, still without styling: subscribe to a slice of the store, and keep a
 * scrolling list at its end while it grows.
 */
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react"
import type { ChatItem, ChatState } from "./reduce.ts"
import type { ChatStore } from "./store.ts"

/**
 * A slice of the conversation; the component redraws only when the slice changes identity.
 * Select what the component draws and nothing more: `s => s.ids` for the list,
 * `s => s.items[id]` for one row, `s => s.run !== null` for the composer.
 */
export function useChat<T>(store: ChatStore, select: (s: ChatState) => T): T {
  const get = () => select(store.get())
  return useSyncExternalStore(store.subscribe, get, get)
}

/** The order of the items: changes only when one is added or removed. */
export const useChatIds = (store: ChatStore) => useChat(store, (s) => s.ids)

/** One item: changes only when that item does. */
export const useChatItem = (store: ChatStore, id: string): ChatItem | undefined => useChat(store, (s) => s.items[id])

/** True while a run is followed. */
export const useChatBusy = (store: ChatStore) => useChat(store, (s) => s.run !== null)

/** How close to the end counts as "at the end", in pixels. */
const SLACK = 48

/**
 * Keep a scrolling list at its end while its content grows, unless the person has scrolled
 * up to read; scrolling back down (or scrollToBottom) resumes following. Put viewportRef on
 * the element that scrolls and contentRef on the element inside it that grows.
 */
export function useStickToBottom<V extends HTMLElement = HTMLDivElement, C extends HTMLElement = HTMLDivElement>() {
  const viewportRef = useRef<V>(null)
  const contentRef = useRef<C>(null)
  const stick = useRef(true)
  const [atBottom, setAtBottom] = useState(true)

  useEffect(() => {
    const view = viewportRef.current
    const content = contentRef.current
    if (!view || !content) return
    const onScroll = () => {
      const near = view.scrollHeight - view.scrollTop - view.clientHeight <= SLACK
      stick.current = near
      setAtBottom(near)
    }
    const grown = new ResizeObserver(() => {
      if (stick.current) view.scrollTop = view.scrollHeight
    })
    view.addEventListener("scroll", onScroll, { passive: true })
    grown.observe(content)
    view.scrollTop = view.scrollHeight
    return () => {
      view.removeEventListener("scroll", onScroll)
      grown.disconnect()
    }
  }, [])

  const scrollToBottom = useCallback(() => {
    const view = viewportRef.current
    stick.current = true
    setAtBottom(true)
    view?.scrollTo({ top: view.scrollHeight, behavior: "smooth" })
  }, [])

  return { viewportRef, contentRef, atBottom, scrollToBottom }
}
