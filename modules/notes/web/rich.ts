/**
 * What Notes does to a note's Markdown before it is drawn, after Basalisk's Notes: [[name]] and
 * [[name|label]] become links the view opens, and relative links and images are resolved against
 * the note's own folder (an image through the notes' file route, a note link to that note).
 * Code (fenced blocks and inline spans) is left as written.
 */

/** A link to a note by its [[name]]. */
export const WIKI = "#note:"
/** A link to a note (or other file) by its path in the notes folder. */
export const FILE = "#note-file:"

const IMAGE = /\.(png|jpe?g|gif|webp|svg|avif|bmp|ico)$/i

/** a/b + ../c.md is a/c.md; null if it climbs above the folder. */
export function joinPath(dir: string, rel: string): string | null {
  const out = dir ? dir.split("/") : []
  for (const part of rel.split("/")) {
    if (part === "" || part === ".") continue
    if (part === "..") {
      if (!out.length) return null
      out.pop()
    } else out.push(part)
  }
  return out.join("/")
}

/** A destination rewritten for the note it is in, or null to leave it alone. */
function resolveDest(dest: string, dir: string, image: boolean, asset: (path: string) => string): string | null {
  const bare = dest.startsWith("<") && dest.endsWith(">") ? dest.slice(1, -1) : dest
  // Absolute URLs, anchors in the note and site paths are left as written.
  if (!bare || /^[a-z][a-z0-9+.-]*:/i.test(bare) || bare.startsWith("#") || bare.startsWith("/")) return null
  const hashAt = bare.indexOf("#")
  const rawPath = hashAt >= 0 ? bare.slice(0, hashAt) : bare
  const hash = hashAt >= 0 ? bare.slice(hashAt + 1) : ""
  let decoded = rawPath
  try {
    decoded = decodeURI(rawPath)
  } catch {
    // Keep it as written.
  }
  const path = joinPath(dir, decoded)
  if (path === null || path === "") return null
  if (image || (IMAGE.test(path) && !hash)) return `<${asset(path)}>`
  return `<${FILE}${encodeURIComponent(path)}${hash ? `#${encodeURIComponent(hash)}` : ""}>`
}

const INLINE = /(!?)\[((?:[^[\]\n]|\[[^\]\n]*\])*)\]\(\s*(<[^>\n]*>|[^)\s]+)/g
const DEFINITION = /^( {0,3}\[[^\]\n]+\]:[ \t]*)(<[^>\n]*>|\S+)/gm
const WIKILINK = /\[\[([^\]|#]+)(?:\|([^\]]*))?\]\]/g

function rewriteProse(text: string, dir: string, asset: (path: string) => string): string {
  // Inline code spans are left as written.
  return text
    .split(/(`[^`\n]*`)/)
    .map((part, i) =>
      i % 2
        ? part
        : part
            .replace(INLINE, (all, bang: string, label: string, dest: string) => {
              const to = resolveDest(dest, dir, bang === "!", asset)
              return to ? `${bang}[${label}](${to}` : all
            })
            .replace(DEFINITION, (all, head: string, dest: string) => {
              const to = resolveDest(dest, dir, false, asset)
              return to ? `${head}${to}` : all
            })
            .replace(WIKILINK, (_m, name: string, label?: string) => {
              const n = name.trim()
              return `[${(label ?? n).trim()}](${WIKI}${encodeURIComponent(n)})`
            }),
    )
    .join("")
}

/**
 * The note's Markdown as it is drawn. path is the note's path in the folder; asset gives the
 * URL that serves a file of the folder.
 */
export function noteMarkdown(source: string, path: string, asset: (path: string) => string): string {
  const dir = path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : ""
  const out: string[] = []
  let prose: string[] = []
  let fence: string | null = null
  const flush = () => {
    if (prose.length) out.push(rewriteProse(prose.join("\n"), dir, asset))
    prose = []
  }
  for (const line of source.split("\n")) {
    const m = /^ {0,3}(`{3,}|~{3,})/.exec(line)
    if (fence === null && m) {
      flush()
      fence = m[1]
      out.push(line)
    } else if (fence !== null) {
      out.push(line)
      if (m && m[1][0] === fence[0] && m[1].length >= fence.length && !line.trim().slice(m[1].length).trim())
        fence = null
    } else prose.push(line)
  }
  flush()
  return out.join("\n")
}
