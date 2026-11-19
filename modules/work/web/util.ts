/** Small helpers the Work views share. */
import { ApiError } from "@hollis-labs/surface-page/sdk"

export const message = (e: unknown) => (e instanceof Error ? e.message : String(e))

/** A 409 from the server (a conflict, or open notes at sign-off). */
export const isConflict = (e: unknown) => e instanceof ApiError && e.status === 409

export function ago(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ""
  const m = Math.round((Date.now() - t) / 60000)
  if (m < 1) return "just now"
  if (m < 60) return `${m}m ago`
  const h = Math.round(m / 60)
  return h < 48 ? `${h}h ago` : `${Math.round(h / 24)}d ago`
}

/** A stage or value as a column heading. */
export const label = (s: string) => (s ? s[0].toUpperCase() + s.slice(1).replace(/-/g, " ") : "")

export const slug = (s: string) =>
  s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")

/** A local date as YYYY-MM-DD. */
export const ymd = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`

/** The drag payload type for an item. */
export const DRAG_TYPE = "text/x-work-item"

export const newId = () => {
  const abc = "abcdefghijklmnopqrstuvwxyz0123456789"
  const b = new Uint8Array(8)
  crypto.getRandomValues(b)
  return Array.from(b, (x) => abc[x % abc.length]).join("")
}

export type DiffPart = { op: "same" | "del" | "ins"; text: string }

/** A word-level diff (longest common subsequence over words and the spaces between). */
export function diffWords(a: string, b: string): DiffPart[] {
  const x = a.split(/(\s+)/)
  const y = b.split(/(\s+)/)
  if (x.length * y.length > 400_000)
    return [
      { op: "del", text: a },
      { op: "ins", text: b },
    ]
  const n = x.length
  const m = y.length
  const t: number[][] = Array.from({ length: n + 1 }, () => new Array(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--)
    for (let j = m - 1; j >= 0; j--) t[i][j] = x[i] === y[j] ? t[i + 1][j + 1] + 1 : Math.max(t[i + 1][j], t[i][j + 1])
  const out: DiffPart[] = []
  const push = (op: DiffPart["op"], text: string) => {
    const last = out[out.length - 1]
    if (last && last.op === op) last.text += text
    else out.push({ op, text })
  }
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (x[i] === y[j]) {
      push("same", x[i])
      i++
      j++
    } else if (t[i + 1][j] >= t[i][j + 1]) push("del", x[i++])
    else push("ins", y[j++])
  }
  while (i < n) push("del", x[i++])
  while (j < m) push("ins", y[j++])
  return out
}
