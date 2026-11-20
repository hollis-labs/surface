/**
 * A JSON document drawn to be read (structured.ts shapes it): one-line values in a label and
 * value grid, longer ones as sections, a list of objects as sub-cards titled by their most
 * telling field. Text is set as Markdown when it looks like Markdown, else as written, with
 * its references as chips. Dates are short ("3d ago"), the full time on hover.
 */
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Markdown } from "@/components/ui/markdown"
import { type DocField, type DocItem, type DocNode, isInline, LIST_PAGE } from "./structured.ts"
import { ago, CiteText } from "./ui.tsx"

function Scalar({ node }: { node: Extract<DocNode, { type: "scalar" }> }) {
  if (node.iso)
    return (
      <time dateTime={node.iso} title={node.iso}>
        {ago(node.iso) ?? node.text}
      </time>
    )
  if (node.empty) return <span className="text-muted-foreground italic">{node.text}</span>
  return (
    <span className={node.mono ? "font-mono text-xs break-all" : "break-words"}>
      <CiteText text={node.text} />
    </span>
  )
}

function Values({ values }: { values: string[] }) {
  if (values.length === 0) return <span className="text-muted-foreground italic">none</span>
  return (
    <span className="flex flex-wrap gap-1">
      {values.map((v, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: values of one fixed document, in order
        <Badge key={i} variant="secondary">
          {v}
        </Badge>
      ))}
    </span>
  )
}

function Text({ node }: { node: Extract<DocNode, { type: "text" }> }) {
  return node.markdown ? (
    <Markdown>{node.text}</Markdown>
  ) : (
    <p className="text-sm leading-relaxed break-words whitespace-pre-wrap">
      <CiteText text={node.text} />
    </p>
  )
}

/** Fields: the one-line ones in a grid, then each longer one as a section under its label. */
export function DocFields({ fields }: { fields: DocField[] }) {
  const inline = fields.filter(isInline)
  const blocks = fields.filter((f) => !isInline(f))
  return (
    <div className="flex min-w-0 flex-col gap-4">
      {inline.length > 0 ? (
        <dl className="grid grid-cols-[minmax(5.5rem,max-content)_minmax(0,1fr)] gap-x-4 gap-y-1 text-sm">
          {inline.map((f) => (
            <div key={f.key} className="contents">
              <dt className="text-muted-foreground">{f.label}</dt>
              <dd className="min-w-0">
                <DocValue node={f.node} />
              </dd>
            </div>
          ))}
        </dl>
      ) : null}
      {blocks.map((f) => (
        <section key={f.key} className="flex min-w-0 flex-col gap-2">
          <h3 className="flex items-center gap-2 text-xs font-medium tracking-wider text-muted-foreground uppercase">
            {f.label}
            {f.node.type === "list" ? <Badge variant="secondary">{f.node.items.length}</Badge> : null}
          </h3>
          <DocValue node={f.node} />
        </section>
      ))}
    </div>
  )
}

/** One value, whatever its shape. */
export function DocValue({ node }: { node: DocNode }) {
  switch (node.type) {
    case "scalar":
      return <Scalar node={node} />
    case "values":
      return <Values values={node.values} />
    case "text":
      return <Text node={node} />
    case "fields":
      return node.fields.length ? (
        <div className="border-l border-border pl-3">
          <DocFields fields={node.fields} />
        </div>
      ) : (
        <span className="text-muted-foreground italic">empty</span>
      )
    case "list":
      return <DocList items={node.items} />
    case "json":
      return <Markdown>{`\`\`\`json\n${node.text}\n\`\`\``}</Markdown>
  }
}

/** A list of sub-cards, LIST_PAGE at a time. */
function DocList({ items }: { items: DocItem[] }) {
  const [shown, setShown] = useState(LIST_PAGE)
  const left = items.length - shown
  return (
    <div className="flex min-w-0 flex-col gap-2">
      {items.slice(0, shown).map((item, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: items of one fixed document, in order
        <Card key={i} size="sm">
          <CardHeader>
            <CardTitle>{item.title}</CardTitle>
            {item.subtitle ? (
              <CardDescription>
                <code className="font-mono text-xs break-all">{item.subtitle}</code>
              </CardDescription>
            ) : null}
          </CardHeader>
          {item.fields.length ? (
            <CardContent>
              <DocFields fields={item.fields} />
            </CardContent>
          ) : null}
        </Card>
      ))}
      {left > 0 ? (
        <Button variant="outline" size="sm" className="self-start" onClick={() => setShown((n) => n + LIST_PAGE)}>
          Show {Math.min(left, LIST_PAGE)} more of {left}
        </Button>
      ) : null}
    </div>
  )
}
