import * as React from "react"
import ReactMarkdown, { type Components } from "react-markdown"
import remarkGfm from "remark-gfm"
import { cn } from "cn"

/**
 * Markdown as a model writes it, set in the theme. Surface's own, after the pattern of AI
 * Elements' Response: the text is cut into blocks and each block is drawn once, so while a
 * message streams only its last block is parsed again. With `streaming`, a last block that
 * is half written (an open `code` span, an open **bold**) is closed for drawing.
 */

const plugins = [remarkGfm]

const components: Components = {
  h1: ({ node: _n, className, ...p }) => <h1 className={cn("mt-4 mb-2 text-base font-semibold first:mt-0", className)} {...p} />,
  h2: ({ node: _n, className, ...p }) => <h2 className={cn("mt-4 mb-2 text-sm font-semibold first:mt-0", className)} {...p} />,
  h3: ({ node: _n, className, ...p }) => <h3 className={cn("mt-3 mb-1.5 text-sm font-semibold first:mt-0", className)} {...p} />,
  p: ({ node: _n, className, ...p }) => <p className={cn("my-2 leading-relaxed first:mt-0 last:mb-0", className)} {...p} />,
  ul: ({ node: _n, className, ...p }) => <ul className={cn("my-2 list-disc pl-5 first:mt-0 last:mb-0", className)} {...p} />,
  ol: ({ node: _n, className, ...p }) => <ol className={cn("my-2 list-decimal pl-5 first:mt-0 last:mb-0", className)} {...p} />,
  li: ({ node: _n, className, ...p }) => <li className={cn("my-0.5 leading-relaxed", className)} {...p} />,
  a: ({ node: _n, className, ...p }) => (
    <a className={cn("text-primary underline underline-offset-2", className)} target="_blank" rel="noreferrer noopener" {...p} />
  ),
  blockquote: ({ node: _n, className, ...p }) => (
    <blockquote className={cn("my-2 border-l-2 border-border pl-3 text-muted-foreground", className)} {...p} />
  ),
  pre: ({ node: _n, className, ...p }) => (
    <pre className={cn("my-2 overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed", className)} {...p} />
  ),
  code: ({ node: _n, className, ...p }) => (
    <code
      className={cn("rounded-sm bg-muted px-1 py-0.5 font-mono text-[0.85em] in-[pre]:bg-transparent in-[pre]:p-0 in-[pre]:text-xs", className)}
      {...p}
    />
  ),
  table: ({ node: _n, className, ...p }) => (
    <div className="my-2 overflow-x-auto">
      <table className={cn("w-full border-collapse text-xs", className)} {...p} />
    </div>
  ),
  th: ({ node: _n, className, ...p }) => <th className={cn("border border-border bg-muted px-2 py-1 text-left font-medium", className)} {...p} />,
  td: ({ node: _n, className, ...p }) => <td className={cn("border border-border px-2 py-1", className)} {...p} />,
  hr: ({ node: _n, className, ...p }) => <hr className={cn("my-4 border-border", className)} {...p} />,
  // An image is a link to it, not a request for it: a remote image loads from its sender's
  // server the moment the text is shown (a read receipt, the reader's address). images opts in.
  img: ({ node: _n, src, alt }) => (
    <a className="text-primary underline underline-offset-2" href={typeof src === "string" ? src : undefined} target="_blank" rel="noreferrer noopener">
      image: {alt || "untitled"}
    </a>
  ),
}

/** With images: drawn, for text whose images are known to be safe to fetch. The key is removed,
 * not set to undefined: react-markdown takes a present key as the component. */
const { img: _img, ...withImages } = components

const fenceRe = /^\s*(```|~~~)/

/** Cut Markdown into top-level blocks at blank lines, never inside a fence or a list item's continuation. */
function splitBlocks(markdown: string): string[] {
  const blocks: string[] = []
  let current: string[] = []
  let fence = ""
  let gap = false
  for (const line of markdown.split("\n")) {
    const f = fenceRe.exec(line)
    if (!fence && line.trim() === "") {
      gap = current.length > 0
      if (gap) current.push(line)
      continue
    }
    if (!fence && gap && !/^[ \t]/.test(line)) {
      while (current.length && current[current.length - 1].trim() === "") current.pop()
      blocks.push(current.join("\n"))
      current = []
    }
    gap = false
    if (f) fence = fence ? (f[1] === fence ? "" : fence) : f[1]
    current.push(line)
  }
  while (current.length && current[current.length - 1].trim() === "") current.pop()
  if (current.length) blocks.push(current.join("\n"))
  return blocks
}

/** Close what a half-written block leaves open, so it draws as it will once finished. */
function closeOpen(block: string): string {
  if (fenceRe.test(block)) return block
  let out = block
  if ((out.match(/`/g)?.length ?? 0) % 2) out += "`"
  if ((out.match(/\*\*/g)?.length ?? 0) % 2) out += "**"
  return out
}

const Block = React.memo(function Block({ text, images }: { text: string; images: boolean }) {
  return (
    <ReactMarkdown remarkPlugins={plugins} components={images ? withImages : components}>
      {text}
    </ReactMarkdown>
  )
})

function Markdown({
  children,
  streaming = false,
  images = false,
  className,
  ...props
}: Omit<React.ComponentProps<"div">, "children"> & {
  children: string
  streaming?: boolean
  /** Draw images (fetching them). Off: each image is a link, so showing text never fetches. */
  images?: boolean
}) {
  const blocks = React.useMemo(() => splitBlocks(children), [children])
  return (
    <div data-slot="markdown" className={cn("min-w-0 text-sm break-words", className)} {...props}>
      {blocks.map((b, i) => (
        <Block key={i} images={images} text={streaming && i === blocks.length - 1 ? closeOpen(b) : b} />
      ))}
    </div>
  )
}

export { Markdown }
