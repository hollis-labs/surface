/**
 * A record whose text is a JSON document (a Caster doc a tool wrote, a registry profile, a
 * delivery trace), as something to read rather than a wall of JSON: an object is labeled
 * fields, a list of objects is a list of sub-cards titled by their most telling field, and
 * a long string is text (Markdown when it looks like Markdown). Pure: structured-view.tsx draws it.
 */

/** One value, shaped for reading. */
export type DocNode =
  /** A short value on one line: a number, a word, an id, a date. */
  | { type: "scalar"; text: string; mono?: boolean; iso?: string; empty?: boolean }
  /** A long string, or one with line breaks: a block of text. */
  | { type: "text"; text: string; markdown: boolean }
  /** An object: its fields, short ones first. */
  | { type: "fields"; fields: DocField[] }
  /** A list of objects: one sub-card each. */
  | { type: "list"; items: DocItem[] }
  /** A list of short values. */
  | { type: "values"; values: string[] }
  /** Too deep to lay out: shown as JSON. */
  | { type: "json"; text: string }

export interface DocField {
  key: string
  label: string
  node: DocNode
}

export interface DocItem {
  title: string
  /** The item's id, when the title is another field. */
  subtitle?: string
  fields: DocField[]
}

/** Text that is a JSON object or array, parsed; anything else (Markdown, a bare value, JSON cut short) is undefined. */
export function parseJSONDocument(text: string | undefined): object | undefined {
  const t = text?.trim() ?? ""
  if (!(t.startsWith("{") && t.endsWith("}")) && !(t.startsWith("[") && t.endsWith("]"))) return undefined
  try {
    const v: unknown = JSON.parse(t)
    return v && typeof v === "object" ? v : undefined
  } catch {
    return undefined
  }
}

/** A JSON key as a label: "created_at" is "Created at", "messageId" is "Message id". */
export function humanize(key: string): string {
  const words = key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim()
    .toLowerCase()
  return words ? words[0].toUpperCase() + words.slice(1) : key
}

const ISO = /^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?$/
const IDLIKE =
  /^(?:[0-9A-HJKMNP-TV-Z]{26}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[0-9a-f]{16,64}|[a-z][a-z0-9+.-]*:\/\/\S+|[A-Z]{2,4}-\d{8}-\d{4})$/

/** A string longer than this, or with a line break, is a block of text. */
export const LONG_TEXT = 120

/** Whether a block of text reads as Markdown (a heading, a list, a fence, emphasis, a link). */
export function looksLikeMarkdown(s: string): boolean {
  return /^(#{1,6} |[-*+] |\d+\. |```|> |\|.*\|\s*$)/m.test(s) || /\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\)/.test(s)
}

/** The fields a sub-card is titled by, in the order they are preferred. */
const TITLE_KEYS = ["title", "subject", "name", "label", "heading", "summary"]
const ID_KEYS = ["id", "key", "ref", "slug", "uuid"]

function scalar(v: unknown): DocNode {
  if (v === null || v === undefined) return { type: "scalar", text: "none", empty: true }
  if (typeof v === "boolean") return { type: "scalar", text: v ? "yes" : "no" }
  if (typeof v === "number") return { type: "scalar", text: v.toLocaleString() }
  const s = String(v)
  if (s === "") return { type: "scalar", text: "empty", empty: true }
  if (ISO.test(s) && !Number.isNaN(Date.parse(s))) return { type: "scalar", text: s, iso: s }
  return { type: "scalar", text: s, mono: IDLIKE.test(s) }
}

const isObject = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v)
const isScalar = (v: unknown) => v === null || typeof v !== "object"

function shortText(v: unknown): string | undefined {
  if (typeof v === "string" && v.trim() && v.length <= LONG_TEXT && !v.includes("\n")) return v.trim()
  if (typeof v === "number") return String(v)
  return undefined
}

/** A sub-card's title and id, and which keys they came from. */
export function titleOf(
  o: Record<string, unknown>,
  index: number,
): { title: string; subtitle?: string; used: string[] } {
  const id = ID_KEYS.find((k) => shortText(o[k]))
  const named = TITLE_KEYS.find((k) => shortText(o[k]))
  if (named) {
    const title = shortText(o[named]) as string
    return id ? { title, subtitle: shortText(o[id]), used: [named, id] } : { title, used: [named] }
  }
  if (id) return { title: shortText(o[id]) as string, used: [id] }
  return { title: `Item ${index + 1}`, used: [] }
}

/** Whether a field is drawn on one line (in the label/value grid) rather than as a section. */
export const isInline = (f: DocField) =>
  f.node.type === "scalar" || (f.node.type === "values" && f.node.values.length <= 8)

const MAX_DEPTH = 5

/** A JSON value shaped for reading. */
export function toDocNode(v: unknown, depth = 0): DocNode {
  if (isScalar(v)) {
    if (typeof v === "string" && (v.length > LONG_TEXT || v.includes("\n")))
      return { type: "text", text: v, markdown: looksLikeMarkdown(v) }
    return scalar(v)
  }
  if (depth >= MAX_DEPTH) return { type: "json", text: JSON.stringify(v, null, 2) }
  if (Array.isArray(v)) {
    if (v.length > 0 && v.every(isObject))
      return {
        type: "list",
        items: v.map((o, i) => {
          const { title, subtitle, used } = titleOf(o, i)
          const rest = Object.fromEntries(Object.entries(o).filter(([k]) => !used.includes(k)))
          return { title, ...(subtitle ? { subtitle } : {}), fields: fieldsOf(rest, depth + 1) }
        }),
      }
    if (v.every(isScalar) && v.every((x) => typeof x !== "string" || (x.length <= LONG_TEXT && !x.includes("\n"))))
      return { type: "values", values: v.map((x) => (scalar(x) as { text: string }).text) }
    return {
      type: "fields",
      fields: v.map((x, i) => ({ key: String(i), label: `${i + 1}`, node: toDocNode(x, depth + 1) })),
    }
  }
  return { type: "fields", fields: fieldsOf(v as Record<string, unknown>, depth) }
}

/** An object's fields: the one-line values first, then the blocks, each group in the document's order. */
export function fieldsOf(o: Record<string, unknown>, depth = 0): DocField[] {
  const all = Object.entries(o).map(([key, val]) => ({ key, label: humanize(key), node: toDocNode(val, depth + 1) }))
  return [...all.filter(isInline), ...all.filter((f) => !isInline(f))]
}

/**
 * Markdown's YAML front matter, split off: its simple `key: value` lines as fields, and the
 * text after it. Undefined when the text has none.
 */
export function splitFrontMatter(text: string): { fields: DocField[]; rest: string } | undefined {
  const m = /^\s*---\r?\n([\s\S]*?)\r?\n---[ \t]*(?:\r?\n|$)/.exec(text)
  if (!m) return undefined
  const fields: DocField[] = []
  for (const line of m[1].split(/\r?\n/)) {
    const kv = /^([A-Za-z0-9_.-]+):[ \t]*(.*)$/.exec(line)
    if (!kv) continue
    const value = kv[2].trim().replace(/^(["'])(.*)\1$/, "$2")
    if (value) fields.push({ key: kv[1], label: humanize(kv[1]), node: scalar(value) })
  }
  return { fields, rest: text.slice(m[0].length) }
}
