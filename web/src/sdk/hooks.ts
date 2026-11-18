import { useEffect, useRef, useState } from "react"
import { getSetting, listMarks, onEvent } from "./runtime"
import type { Mark, SurfaceEvent } from "./types"

/**
 * Listen to the app's event bus. fn is called for each event whose topic is prefix or lies
 * under it ("notes" takes "notes.saved"). This is how a view learns that the agent, a
 * command or another window changed its data.
 */
export function useEvent<T = unknown>(prefix: string, fn: (ev: SurfaceEvent<T>) => void): void {
  const ref = useRef(fn)
  ref.current = fn
  useEffect(() => onEvent(prefix, (ev) => ref.current(ev as SurfaceEvent<T>)), [prefix])
}

/** A live setting by its full key ("scratch.show_done"). It follows changes, with no reload. */
export function useSetting<T = unknown>(fullKey: string): T | undefined {
  const [value, setValue] = useState<T | undefined>(() => getSetting(fullKey) as T | undefined)
  useEffect(() => {
    setValue(getSetting(fullKey) as T | undefined)
    return onEvent("config.changed", (ev) => {
      const d = ev.data as { key: string; value: T }
      if (d?.key === fullKey) setValue(d.value)
    })
  }, [fullKey])
  return value
}

/** The marks on a ref, kept current. */
export function useMarks(ref: string | undefined): Mark[] {
  const [marks, setMarks] = useState<Mark[]>([])
  useEffect(() => {
    if (!ref) {
      setMarks([])
      return
    }
    let live = true
    const load = () =>
      listMarks(ref).then((m) => {
        if (live) setMarks(m)
      })
    void load()
    const off = onEvent("marks.changed", (ev) => {
      if ((ev.data as { ref?: string })?.ref === ref) void load()
    })
    return () => {
      live = false
      off()
    }
  }, [ref])
  return marks
}
