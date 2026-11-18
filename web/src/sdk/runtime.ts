/**
 * The connection between the page and the app's server: one event stream, the settings as
 * last heard, and plain JSON calls. The desk (../desk) builds on it; a module reaches it
 * only through the hooks and what its views are handed.
 */
import type { Mark, SurfaceEvent } from "./types"

export class ApiError extends Error {
  status: number
  body: unknown
  constructor(message: string, status: number, body: unknown) {
    super(message)
    this.status = status
    this.body = body
  }
}

/** A JSON call to the app's server. Throws ApiError with the server's own words. */
export async function call<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let data: unknown = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = text
  }
  if (!res.ok) {
    const msg = (data as { error?: string } | null)?.error ?? `${res.status} ${res.statusText}`
    throw new ApiError(msg, res.status, data)
  }
  return data as T
}

type Listener = (ev: SurfaceEvent) => void
const listeners = new Set<{ prefix: string; fn: Listener }>()

const matches = (prefix: string, topic: string) => prefix === "" || topic === prefix || topic.startsWith(`${prefix}.`)

/** Listen to the bus. Returns the function that stops listening. */
export function onEvent(prefix: string, fn: Listener): () => void {
  const entry = { prefix, fn }
  listeners.add(entry)
  return () => {
    listeners.delete(entry)
  }
}

function dispatch(ev: SurfaceEvent) {
  for (const l of [...listeners]) {
    if (!matches(l.prefix, ev.topic)) continue
    try {
      l.fn(ev)
    } catch (err) {
      console.error("surface: an event listener failed", ev.topic, err)
    }
  }
}

let source: EventSource | null = null
let connected = false
const connectionListeners = new Set<(up: boolean) => void>()

/** Whether the event stream is up, and a way to hear when that changes. */
export const isConnected = () => connected
export function onConnection(fn: (up: boolean) => void): () => void {
  connectionListeners.add(fn)
  return () => {
    connectionListeners.delete(fn)
  }
}

/** Open the event stream (once). The server sends the desk's state first on every connect. */
export function connect() {
  if (source) return
  source = new EventSource("/api/events")
  source.addEventListener("surface", (e) => {
    if (!connected) {
      connected = true
      for (const fn of connectionListeners) fn(true)
    }
    try {
      dispatch(JSON.parse((e as MessageEvent).data) as SurfaceEvent)
    } catch (err) {
      console.error("surface: bad event", err)
    }
  })
  source.onopen = () => {
    connected = true
    for (const fn of connectionListeners) fn(true)
  }
  source.onerror = () => {
    // EventSource reconnects by itself; the state arrives again when it does.
    connected = false
    for (const fn of connectionListeners) fn(false)
  }
}

// Settings, as last heard.
export interface SettingEntry {
  scope: string
  key: string
  full_key: string
  title: string
  about?: string
  type: "string" | "bool" | "int" | "enum"
  default: unknown
  options?: string[]
  value: unknown
  set: boolean
}

const settings = new Map<string, SettingEntry>()

export async function loadSettings(): Promise<SettingEntry[]> {
  const list = await call<SettingEntry[]>("GET", "/api/settings")
  settings.clear()
  for (const s of list) settings.set(s.full_key, s)
  return list
}
export const getSetting = (fullKey: string): unknown => settings.get(fullKey)?.value
export const allSettings = (): SettingEntry[] => [...settings.values()]

onEvent("config.changed", (ev) => {
  const d = ev.data as { key: string; value: unknown }
  const s = settings.get(d.key)
  if (s) settings.set(d.key, { ...s, value: d.value, set: true })
})

/** Change a live setting by its full key ("files.show_hidden"). Every window hears it. */
export async function setSetting(fullKey: string, value: unknown): Promise<void> {
  await call("PUT", `/api/settings/${encodeURIComponent(fullKey)}`, { value })
}

export const listMarks = (ref: string) => call<Mark[]>("GET", `/api/marks?ref=${encodeURIComponent(ref)}`)
