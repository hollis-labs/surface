/**
 * remarkCite: references in Markdown become cite elements, which the Markdown component's
 * `cite` entry draws as chips (./markdown.tsx). Dependency-free: it walks mdast as plain
 * objects, so it is tested with node:test and needs no unified types.
 *
 * - Prose: each text node is split into text and references. Text inside a link stays as it is.
 * - Links: `[label](tach:<service>/<id>)` becomes a chip with that label; a bare URL (an autolink,
 *   its text the URL itself) becomes a chip, and so does a link that is a reference of another
 *   kind (a fragment's reader link). A labelled link to a web page stays a link.
 * - Code: fenced code is left alone; inline code that is exactly one reference (`CW-…`,
 *   `~/dev/app/main.go`) becomes a chip set in the monospace face.
 */
import { detect, detectRefs, parseTachHref, type RefRegistry } from "./core/index.ts"

interface MdNode {
  type: string
  value?: string
  url?: string
  children?: MdNode[]
  data?: Record<string, unknown>
}

/** The element the plugin makes, and the attributes it carries. */
export const CITE_ELEMENT = "cite"
export const CITE_ATTR = {
  kind: "data-cite-kind",
  id: "data-cite-id",
  label: "data-cite-label",
  mono: "data-cite-mono",
} as const

function citeNode(kind: string, canonical: string, raw: string, label?: string, mono?: boolean): MdNode {
  const props: Record<string, string> = { dataCiteKind: kind, dataCiteId: canonical }
  if (label) props.dataCiteLabel = label
  if (mono) props.dataCiteMono = "true"
  return { type: "cite", data: { hName: CITE_ELEMENT, hProperties: props }, children: [{ type: "text", value: raw }] }
}

function textOf(n: MdNode): string {
  if (typeof n.value === "string") return n.value
  return (n.children ?? []).map(textOf).join("")
}

const SKIP = new Set(["code", "html", "definition", "linkReference", "imageReference", "image", "cite"])

/** The transform, over a registry of kinds. */
export function citeTransform(registry: RefRegistry, tree: MdNode): void {
  const walk = (parent: MdNode) => {
    const kids = parent.children
    if (!kids) return
    const out: MdNode[] = []
    for (const n of kids) {
      if (SKIP.has(n.type)) {
        out.push(n)
        continue
      }
      if (n.type === "text" && n.value) {
        for (const s of detect(n.value, registry, { skipCode: false })) {
          out.push(s.type === "text" ? { type: "text", value: s.text } : citeNode(s.kind, s.canonical, s.raw))
        }
        continue
      }
      if (n.type === "inlineCode" && n.value) {
        const v = n.value.trim()
        const refs = detectRefs(v, registry, { skipCode: false })
        if (refs.length === 1 && refs[0].start === 0 && refs[0].end === v.length && refs[0].kind !== "url") {
          out.push(citeNode(refs[0].kind, refs[0].canonical, refs[0].raw, undefined, true))
        } else {
          out.push(n)
        }
        continue
      }
      if (n.type === "link") {
        const label = textOf(n).trim()
        const tach = parseTachHref(n.url)
        if (tach) {
          out.push(citeNode(tach.kind, tach.canonical, label || tach.canonical, label || undefined))
          continue
        }
        const url = n.url ?? ""
        const refs = url ? detectRefs(url, registry, { skipCode: false }) : []
        const whole = refs.length === 1 && refs[0].start === 0 && refs[0].end === url.length ? refs[0] : null
        // A bare URL (its text is the address), or a link that is a reference of another kind.
        const bare =
          label === url ||
          label === url.replace(/^mailto:/, "") ||
          `https://${label}` === url ||
          `http://${label}` === url
        if (whole && (whole.kind !== "url" || bare)) {
          out.push(citeNode(whole.kind, whole.canonical, label || whole.raw, bare ? undefined : label))
          continue
        }
        out.push(n) // a labelled link to a page: a link; its text is left alone
        continue
      }
      walk(n)
      out.push(n)
    }
    parent.children = out
  }
  walk(tree)
}

/** A remark plugin over a registry: `remarkPlugins={[createRemarkCite(registry)]}`. */
export function createRemarkCite(registry: RefRegistry) {
  return function remarkCite() {
    return (tree: unknown) => citeTransform(registry, tree as MdNode)
  }
}
