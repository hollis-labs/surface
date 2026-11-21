/**
 * The record view (surface.cite) and the slide-over Cite Object Drawer (<CiteDrawer />):
 * a cited record read whole, with a compact header and reusable <ObjectActionsMenu />.
 */
import { Braces, ExternalLink, ListTree, RefreshCw } from "lucide-react"
import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { CiteRecordHeader, type CiteTone } from "@/components/ui/cite-card"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Markdown } from "@/components/ui/markdown"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Spinner } from "@/components/ui/spinner"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { ObjectActionsMenu } from "../desk/ObjectActionsMenu"
import type { ViewProps } from "../sdk/types"
import { buildPreviewCard } from "./core/index.ts"
import {
  type CiteRecord,
  canResolve,
  closeCiteDrawer,
  kindLabel,
  loadRecord,
  openURL,
  registry,
  useCiteDrawer,
  useRefState,
} from "./store.ts"
import { fenced, fitsLayout, parseJSONDocument, splitFrontMatter, toDocNode } from "./structured.ts"
import { DocFields, DocValue } from "./structured-view.tsx"
import { CiteText, iconFor, subtitleFor, updatedAgo } from "./ui.tsx"

/**
 * A record's text: Markdown (its front matter, if any, as fields above it), or, when it is a
 * JSON document (a collab/v1 one has front matter over it), its fields laid out to be read,
 * with the JSON a toggle away, shown as it was written. A document too large to lay out is
 * shown as its text.
 */
export function RecordBody({ body }: { body: string }) {
  const front = useMemo(() => (parseJSONDocument(body) ? undefined : splitFrontMatter(body)), [body])
  const json = front ? front.rest : body
  const doc = useMemo(() => parseJSONDocument(json), [json])
  const fits = useMemo(() => (doc ? fitsLayout(json, doc) : false), [json, doc])
  const node = useMemo(() => (doc && fits ? toDocNode(doc) : undefined), [doc, fits])
  const [mode, setMode] = useState<"fields" | "raw">("fields")
  const head = front?.fields.length ? <DocFields fields={front.fields} /> : null
  if (doc && !fits)
    return (
      <div className="flex min-w-0 flex-col gap-3">
        {head}
        <p className="text-xs text-muted-foreground">
          This JSON document is too large to lay out; it is shown as it is.
        </p>
        <pre className="font-mono text-xs break-all whitespace-pre-wrap">{json}</pre>
      </div>
    )
  if (!node)
    return front ? (
      <div className="flex min-w-0 flex-col gap-4">
        {head}
        <Markdown>{front.rest}</Markdown>
      </div>
    ) : (
      <Markdown>{body}</Markdown>
    )
  return (
    <div className="flex min-w-0 flex-col gap-3">
      {head}
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
      {mode === "fields" ? <DocValue node={node} /> : <Markdown>{fenced(json.trim(), "json")}</Markdown>}
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
      <div className="border-b border-border bg-card/40 px-3.5 py-2.5">
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
              <Button variant="ghost" size="xs" onClick={load} disabled={busy} title="Reload record">
                <RefreshCw className="size-3.5" />
                Reload
              </Button>
              {rec.url ? (
                <Button variant="ghost" size="xs" onClick={() => openURL(rec.url as string)}>
                  <ExternalLink className="size-3.5" />
                  Source
                </Button>
              ) : null}
              <ObjectActionsMenu
                obj={{
                  kind,
                  id,
                  title: rec.title,
                  subtitle: rec.description || rec.subtitle,
                  url: rec.url,
                  body: rec.body,
                }}
              />
            </>
          }
        />
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-3.5">
        {rec.body ? (
          <RecordBody body={rec.body} />
        ) : (
          <p className="text-muted-foreground">{rec.description || "Nothing more to show."}</p>
        )}
      </div>
    </div>
  )
}

/**
 * Slide-over Cite Object Drawer / Panel: opens whenever the user clicks a Cite chip or clicks
 * "View" on a Cite hover card.
 */
export function CiteDrawer() {
  const active = useCiteDrawer()
  const kind = active?.kind ?? ""
  const id = active?.canonical ?? ""
  const resolvable = Boolean(kind && canResolve(kind))
  const refState = useRefState(kind, id, resolvable)
  const preview = useMemo(() => (active ? buildPreviewCard(registry, active, refState) : null), [active, refState])
  const [rec, setRec] = useState<CiteRecord | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    if (!kind || !id || !resolvable) {
      setRec(null)
      setError("")
      return
    }
    setBusy(true)
    setError("")
    loadRecord({ kind, canonical: id })
      .then((r) => setRec(r))
      .catch((err: Error) => {
        // Some kinds only support preview resolution and not full record fetch; fall back to preview gracefully.
        setRec(null)
        setError(err.message)
      })
      .finally(() => setBusy(false))
  }, [kind, id, resolvable])

  useEffect(() => {
    setRec(null)
    setError("")
    load()
  }, [load])

  const Icon = iconFor(kind)
  const entity = refState.status === "found" ? refState.entity : undefined
  const title = rec?.title ?? preview?.title ?? id
  const subtitle = subtitleFor(kind, rec?.subtitle ?? preview?.subtitle)
  const status = rec?.status ?? preview?.status
  const fields = rec?.fields ?? preview?.fields ?? []
  const updated = updatedAgo(rec?.updatedAt ?? preview?.updatedAt)
  const description = rec?.description ?? preview?.description
  const url = rec?.url ?? entity?.url ?? (kind === "url" ? id : undefined)

  return (
    <Sheet open={Boolean(active)} onOpenChange={(open) => !open && closeCiteDrawer()}>
      <SheetContent
        side="right"
        showCloseButton={true}
        className="w-[min(38rem,92vw)] gap-0 border-l border-border bg-background p-0 sm:max-w-xl"
      >
        <SheetHeader className="sr-only">
          <SheetTitle>{title}</SheetTitle>
          <SheetDescription>{`${kindLabel(kind)}: ${id}`}</SheetDescription>
        </SheetHeader>
        {active && (
          <div className="flex h-full min-h-0 flex-col">
            <div className="border-b border-border bg-card/50 py-2.5 pr-11 pl-3.5">
              <CiteRecordHeader
                icon={<Icon />}
                kindLabel={kindLabel(kind)}
                canonical={id}
                title={title}
                subtitle={subtitle}
                status={status ? { label: status.label, tone: status.tone as CiteTone } : undefined}
                fields={fields}
                renderValue={(f) => (f.mono ? <CiteText text={f.value} /> : f.value)}
                updated={updated}
                error={!rec && !entity && error ? error : undefined}
                actions={
                  <>
                    {resolvable && (
                      <Button variant="ghost" size="icon-xs" onClick={load} disabled={busy} title="Reload">
                        <RefreshCw className="size-3.5" />
                      </Button>
                    )}
                    {url ? (
                      <Button variant="ghost" size="xs" onClick={() => openURL(url)} title="Open external URL">
                        <ExternalLink className="size-3.5" />
                        Source
                      </Button>
                    ) : null}
                    <ObjectActionsMenu
                      obj={{
                        kind,
                        id,
                        title,
                        subtitle: description || subtitle,
                        url,
                        body: rec?.body || description,
                      }}
                    />
                  </>
                }
              />
            </div>
            <div className="min-h-0 flex-1 overflow-auto p-3.5">
              {busy && !rec && !entity ? (
                <div className="grid h-40 place-items-center">
                  <Spinner />
                </div>
              ) : rec?.body ? (
                <RecordBody body={rec.body} />
              ) : description ? (
                <div className="flex flex-col gap-3">
                  <Markdown>{description}</Markdown>
                </div>
              ) : (
                <p className="text-muted-foreground text-xs">
                  {kind === "url"
                    ? "External web address. Use Source or the actions menu above to share, bookmark, or add to context."
                    : "No additional body content for this reference."}
                </p>
              )}
            </div>
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}
