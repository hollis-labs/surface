/**
 * Cite in the page: a reference as a chip with a hover card (CiteRef), plain text with its
 * references as chips (CiteText), and what Markdown needs to do the same (citeMarkdownExtensions).
 */
import {
  Bot,
  Brain,
  Copy,
  ExternalLink,
  FileText,
  Folder,
  Layers,
  Link2,
  ListChecks,
  type LucideIcon,
  Mail,
  Maximize2,
  NotebookText,
  Target,
  Zap,
} from "lucide-react"
import { type ComponentProps, type ReactNode, useState } from "react"
import { Button } from "@/components/ui/button"
import { CiteCard, type CiteTone } from "@/components/ui/cite-card"
import { CiteChip, type CiteChipState } from "@/components/ui/cite-chip"
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card"
import { buildPreviewCard, type RefIdentity } from "./core/index.ts"
import { CITE_ATTR, CITE_ELEMENT, createRemarkCite } from "./remark.ts"
import {
  canResolve,
  kindLabel,
  openCite,
  openRecord,
  openURL,
  registry,
  requestRef,
  segments,
  useRefState,
} from "./store.ts"

const ICONS: Record<string, LucideIcon> = {
  "torque.task": ListChecks,
  "torque.epic": Layers,
  "torque.project": Layers,
  "torque.sprint": Layers,
  "torque.plan": Layers,
  "tesseract.item": Brain,
  "tesseract.key": Brain,
  "tether.agent": Bot,
  "tether.message": Mail,
  "caster.doc": NotebookText,
  "work.item": Target,
  path: FileText,
  "path.relative": FileText,
  url: Link2,
  "fragments.fragment": Zap,
}
export const iconFor = (kind: string): LucideIcon => ICONS[kind] ?? Link2

const clip = (s: string, n: number) => (s.length > n ? `${s.slice(0, n - 1)}…` : s)

/** "3m ago", from an ISO time. */
export function ago(iso?: string): string | undefined {
  if (!iso) return undefined
  const ms = Date.now() - new Date(iso).getTime()
  if (Number.isNaN(ms)) return undefined
  const m = Math.round(ms / 60000)
  if (m < 1) return "just now"
  if (m < 60) return `${m}m ago`
  const h = Math.round(m / 60)
  if (h < 48) return `${h}h ago`
  return `${Math.round(h / 24)}d ago`
}

function hostOf(url: string): string {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

/** The hover card's body for a reference: what it is, as far as is known now. */
export function RefCard({ refId }: { refId: RefIdentity }) {
  const state = useRefState(refId.kind, refId.canonical, canResolve(refId.kind))
  const card = buildPreviewCard(registry, refId, state)
  const Icon =
    refId.kind === "path" && state.status === "found" && state.entity.subtitle?.startsWith("Folder")
      ? Folder
      : iconFor(refId.kind)
  const entity = state.status === "found" ? state.entity : undefined
  const isURL = refId.kind === "url"
  const [copied, setCopied] = useState(false)
  const copy = () =>
    void navigator.clipboard
      ?.writeText(refId.canonical)
      .then(() => setCopied(true))
      .catch(() => undefined)
  const openable = isURL || canResolve(refId.kind)
  return (
    <CiteCard
      phase={card.phase}
      icon={<Icon />}
      kindLabel={kindLabel(refId.kind)}
      canonical={refId.canonical}
      title={isURL ? hostOf(refId.canonical) : card.title}
      subtitle={
        isURL ? "A web page. Its address is shown as written; nothing is fetched to preview it." : card.subtitle
      }
      description={card.description}
      status={card.status ? { label: card.status.label, tone: card.status.tone as CiteTone } : undefined}
      fields={card.fields}
      updated={ago(card.updatedAt)}
      error={card.phase === "error" ? `Could not be looked up: ${card.error}` : undefined}
      actions={
        <>
          <Button variant="ghost" size="xs" onClick={copy}>
            <Copy />
            {copied ? "Copied" : "Copy"}
          </Button>
          {entity?.url ? (
            <Button variant="ghost" size="xs" onClick={() => openURL(entity.url as string)}>
              <ExternalLink />
              {hostOf(entity.url)}
            </Button>
          ) : null}
          {entity?.open && canResolve(refId.kind) ? (
            <Button variant="ghost" size="xs" onClick={() => void openRecord(refId)}>
              <Maximize2 />
              Record
            </Button>
          ) : null}
          {openable ? (
            <Button variant="outline" size="xs" onClick={() => openCite(refId)}>
              {isURL ? <ExternalLink /> : <Maximize2 />}
              Open
            </Button>
          ) : null}
        </>
      }
    />
  )
}

const chipState = (s: string): CiteChipState =>
  s === "found" || s === "not-found" || s === "error" || s === "loading" || s === "unsupported" ? s : "idle"

/**
 * A reference, inline: an icon and a label, with a card on hover or focus, opened on click.
 * Once resolved the label is the record's own ("Task: Fix the thing"); until then it is what
 * was written, or `label` (a Markdown link's text).
 */
export function CiteRef({
  refId,
  raw,
  label,
  mono,
}: {
  refId: RefIdentity
  raw?: string
  label?: string
  mono?: boolean
}) {
  const resolvable = canResolve(refId.kind)
  const state = useRefState(refId.kind, refId.canonical, resolvable)
  const Icon = iconFor(refId.kind)
  const kind = registry.get(refId.kind)
  const written = label ?? kind?.chipLabel?.(refId, raw ?? refId.canonical) ?? raw ?? refId.canonical
  const text = state.status === "found" && state.entity.chipLabel && !label ? state.entity.chipLabel : written
  return (
    <HoverCard onOpenChange={(open) => open && resolvable && requestRef(refId)}>
      <HoverCardTrigger
        delay={300}
        closeDelay={150}
        render={
          <CiteChip
            icon={<Icon aria-hidden="true" />}
            label={clip(text, 56)}
            state={resolvable ? chipState(state.status) : "unsupported"}
            mono={mono && text === written}
            data-kind={refId.kind}
            aria-label={`${kindLabel(refId.kind)}: ${text}`}
            onClick={(e) => {
              e.preventDefault()
              e.stopPropagation()
              openCite(refId)
            }}
          />
        }
      />
      <HoverCardContent className="w-80" align="start">
        <RefCard refId={refId} />
      </HoverCardContent>
    </HoverCard>
  )
}

/** Plain text with its references as chips. For text that is not Markdown. */
export function CiteText({ text }: { text: string }) {
  return (
    <>
      {segments(text).map((s) =>
        s.type === "text" ? (
          s.text
        ) : (
          <CiteRef
            key={`${s.start}:${s.kind}`}
            refId={{ kind: s.kind, canonical: s.canonical, meta: s.meta }}
            raw={s.raw}
          />
        ),
      )}
    </>
  )
}

/** The remark plugin over the page's registry. */
export const remarkCite = createRemarkCite(registry)

/** The `cite` element remarkCite makes, drawn as a chip; any other <cite> as it is. */
function CiteElement({
  node: _node,
  children,
  ...props
}: ComponentProps<"cite"> & { node?: unknown; children?: ReactNode }) {
  const p = props as Record<string, string | undefined>
  const kind = p[CITE_ATTR.kind]
  const id = p[CITE_ATTR.id]
  if (!kind || !id) return <cite {...props}>{children}</cite>
  const raw = typeof children === "string" ? children : Array.isArray(children) ? children.join("") : id
  return (
    <CiteRef refId={{ kind, canonical: id }} raw={raw} label={p[CITE_ATTR.label]} mono={p[CITE_ATTR.mono] === "true"} />
  )
}

/**
 * What Markdown needs to show references as chips: the remark plugin and the component that
 * draws its element. For the Markdown component's `remarkPlugins` and `components`, or its
 * MarkdownExtensions provider (every Markdown below it).
 */
export const citeMarkdownExtensions = {
  remarkPlugins: [remarkCite],
  components: { [CITE_ELEMENT]: CiteElement },
}
