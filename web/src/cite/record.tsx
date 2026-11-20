/**
 * The record window (surface.cite): a cited record read whole, for a reference whose module has
 * no window of its own to show it in (a Torque task, a Tesseract item, a fragment). Args: {kind, id}.
 */
import { Braces, ExternalLink, ListTree, RefreshCw, SquareArrowOutUpRight } from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { CiteRecordHeader, type CiteTone } from "@/components/ui/cite-card"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Markdown } from "@/components/ui/markdown"
import { Spinner } from "@/components/ui/spinner"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import type { ViewProps } from "../sdk/types"
import { type CiteRecord, kindLabel, loadRecord, openURL } from "./store.ts"
import { parseJSONDocument, splitFrontMatter, toDocNode } from "./structured.ts"
import { DocFields, DocValue } from "./structured-view.tsx"
import { CiteText, iconFor, subtitleFor, updatedAgo } from "./ui.tsx"

/**
 * A record's text: Markdown (its front matter, if any, as fields above it), or, when it is a
 * JSON document, its fields laid out to be read, with the raw JSON a toggle away.
 */
export function RecordBody({ body }: { body: string }) {
  const doc = useMemo(() => parseJSONDocument(body), [body])
  const node = useMemo(() => (doc ? toDocNode(doc) : undefined), [doc])
  const front = useMemo(() => (doc ? undefined : splitFrontMatter(body)), [doc, body])
  const [mode, setMode] = useState<"fields" | "raw">("fields")
  if (front)
    return (
      <div className="flex min-w-0 flex-col gap-4">
        {front.fields.length ? <DocFields fields={front.fields} /> : null}
        <Markdown>{front.rest}</Markdown>
      </div>
    )
  if (!node) return <Markdown>{body}</Markdown>
  return (
    <div className="flex min-w-0 flex-col gap-3">
      <ToggleGroup
        variant="outline"
        size="sm"
        spacing={0}
        value={[mode]}
        onValueChange={(v: string[]) => v[0] && setMode(v[0] as "fields" | "raw")}
        aria-label="How the document is shown"
      >
        <ToggleGroupItem value="fields" aria-label="As fields">
          <ListTree />
          Fields
        </ToggleGroupItem>
        <ToggleGroupItem value="raw" aria-label="As raw JSON">
          <Braces />
          JSON
        </ToggleGroupItem>
      </ToggleGroup>
      {mode === "fields" ? (
        <DocValue node={node} />
      ) : (
        <Markdown>{`\`\`\`json\n${JSON.stringify(doc, null, 2)}\n\`\`\``}</Markdown>
      )}
    </div>
  )
}

export function CiteRecordView({ win, desk }: ViewProps) {
  const kind = String(win.args?.kind ?? "")
  const id = String(win.args?.id ?? "")
  const [rec, setRec] = useState<CiteRecord | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const load = useCallback(() => {
    if (!kind || !id) return
    setBusy(true)
    setError("")
    loadRecord({ kind, canonical: id })
      .then((r) => {
        setRec(r)
        void desk.setTitle(win.id, r.chipLabel || r.title)
      })
      .catch((err: Error) => setError(err.message))
      .finally(() => setBusy(false))
  }, [kind, id, desk, win.id])
  useEffect(load, [load])
  useEffect(() => desk.report(win.id, { kind: "record", ref: `cite:${kind}:${id}` }), [desk, win.id, kind, id])

  const Icon = iconFor(kind)
  if (!rec && error)
    return (
      <Empty className="h-full">
        <EmptyHeader>
          <EmptyTitle>{kindLabel(kind)}</EmptyTitle>
          <EmptyDescription>{error}</EmptyDescription>
        </EmptyHeader>
        <Button variant="outline" size="sm" onClick={load}>
          <RefreshCw />
          Try again
        </Button>
      </Empty>
    )
  if (!rec)
    return (
      <div className="grid h-full place-items-center">
        <Spinner />
      </div>
    )
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="border-b border-border p-4">
        <CiteRecordHeader
          icon={<Icon />}
          kindLabel={kindLabel(kind)}
          canonical={id}
          title={rec.title}
          subtitle={subtitleFor(kind, rec.subtitle)}
          status={rec.status ? { label: rec.status.label, tone: rec.status.tone as CiteTone } : undefined}
          fields={rec.fields}
          renderValue={(f) => (f.mono ? <CiteText text={f.value} /> : f.value)}
          updated={updatedAgo(rec.updatedAt)}
          error={error || undefined}
          actions={
            <>
              <Button variant="ghost" size="xs" onClick={load} disabled={busy}>
                <RefreshCw />
                Reload
              </Button>
              {rec.url ? (
                <Button variant="ghost" size="xs" onClick={() => openURL(rec.url as string)}>
                  <ExternalLink />
                  Open the source
                </Button>
              ) : null}
              {rec.open ? (
                <Button
                  variant="outline"
                  size="xs"
                  onClick={() => void desk.run(rec.open?.command ?? "", rec.open?.args ?? {})}
                >
                  <SquareArrowOutUpRight />
                  Open in its window
                </Button>
              ) : null}
            </>
          }
        />
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-4">
        {rec.body ? (
          <RecordBody body={rec.body} />
        ) : (
          <p className="text-muted-foreground">{rec.description || "Nothing more to show."}</p>
        )}
      </div>
    </div>
  )
}
