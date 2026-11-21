/**
 * Cite in the page: a reference as a chip with a hover card (CiteRef), plain text with its
 * references as chips (CiteText), and what Markdown needs to do the same (citeMarkdownExtensions).
 */
import {
  Bot,
  Boxes,
  Brain,
  Copy,
  ExternalLink,
  Eye,
  FileText,
  Folder,
  Layers,
  Link2,
  ListChecks,
  type LucideIcon,
  Mail,
  NotebookText,
  Radar,
  Target,
  Zap,
} from "lucide-react"
import { type ComponentProps, type ReactNode, useState } from "react"
import { Button } from "@/components/ui/button"
import { CiteCard, type CiteTone } from "@/components/ui/cite-card"
import { CiteChip, type CiteChipState } from "@/components/ui/cite-chip"
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card"
import { ObjectActionsMenu } from "../desk/ObjectActionsMenu"
import { buildPreviewCard, type RefIdentity } from "./core/index.ts"
import { CITE_ATTR, CITE_ELEMENT, createRemarkCite } from "./remark.ts"
import { canResolve, kindLabel, openCiteDrawer, openURL, registry, requestRef, segments, useRefState } from "./store.ts"

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
  "registry.record": Boxes,
  "ops.workstream": Radar,
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

/** "updated 3m ago", as the card's foot says it. */
export const updatedAgo = (iso?: string) => {
  const a = ago(iso)
  return a ? `updated ${a}` : undefined
}

/** A subtitle worth showing: not the kind's own name again ("Torque task" under "TORQUE TASK"). */
export const subtitleFor = (kind: string, subtitle?: string) =>
  subtitle && subtitle.trim().toLowerCase() !== kindLabel(kind).toLowerCase() ? subtitle : undefined

function hostOf(url: string): string {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

/** What the card says when there is nothing to show (Basalisk's wording). */
const NOTES: Partial<Record<string, string>> = {
  "not-found": "It may have been deleted, or it may not be readable.",
  unresolved: "No preview for this kind yet.",
}

/** The hover card's body for a reference: what it is, as far as is known now. */
export function RefCard({ refId, onCloseHover }: { refId: RefIdentity; onCloseHover?: () => void }) {
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
  const title =
    card.phase === "not-found"
      ? "Not found"
      : card.phase === "error"
        ? "Could not be looked up"
        : isURL
          ? hostOf(refId.canonical)
          : card.title
  const viewInDrawer = () => {
    onCloseHover?.()
    openCiteDrawer(refId)
  }
  return (
    <CiteCard
      phase={card.phase}
      icon={<Icon />}
      kindLabel={kindLabel(refId.kind)}
      canonical={refId.canonical}
      title={title}
      subtitle={
        isURL
          ? "A web page. Its address is shown as written; nothing is fetched to preview it."
          : card.phase === "ready"
            ? subtitleFor(refId.kind, card.subtitle)
            : undefined
      }
      note={isURL ? undefined : NOTES[card.phase]}
      description={card.description}
      status={card.status ? { label: card.status.label, tone: card.status.tone as CiteTone } : undefined}
      fields={card.fields}
      updated={updatedAgo(card.updatedAt)}
      error={card.phase === "error" ? card.error : undefined}
      actions={
        <>
          <Button
            variant="outline"
            size="xs"
            className="h-6 rounded-sm border-border bg-transparent px-2 font-mono text-[11px] text-foreground/85 hover:border-primary hover:bg-transparent hover:text-foreground"
            onClick={copy}
          >
            <Copy />
            {copied ? "Copied" : "Copy id"}
          </Button>
          {entity?.url ? (
            <Button
              variant="outline"
              size="xs"
              className="h-6 rounded-sm border-border bg-transparent px-2 font-mono text-[11px] text-foreground/85 hover:border-primary hover:bg-transparent hover:text-foreground"
              onClick={() => {
                onCloseHover?.()
                openURL(entity.url as string)
              }}
            >
              <ExternalLink />
              {hostOf(entity.url)}
            </Button>
          ) : null}
          <Button
            variant="outline"
            size="xs"
            className="h-6 rounded-sm border-primary bg-transparent px-2 font-mono text-[11px] text-foreground hover:border-primary hover:bg-accent"
            onClick={viewInDrawer}
          >
            <Eye />
            View
          </Button>
          <ObjectActionsMenu
            obj={{
              kind: refId.kind,
              id: refId.canonical,
              title: card.title,
              subtitle: card.description || card.subtitle,
              url: isURL ? refId.canonical : entity?.url,
            }}
            onAction={() => onCloseHover?.()}
          />
        </>
      }
    />
  )
}

const chipState = (s: string): CiteChipState =>
  s === "found" || s === "not-found" || s === "error" || s === "loading" || s === "unsupported" ? s : "idle"

/**
 * A reference, inline: an icon and a label, with a card on hover or focus, opened in the Cite
 * Object Drawer on click. The inline chip keeps its written label and font metrics stable so
 * async preview resolution never shifts surrounding text or the hover card position.
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
  const [hoverOpen, setHoverOpen] = useState(false)
  const resolvable = canResolve(refId.kind)
  const state = useRefState(refId.kind, refId.canonical, resolvable)
  const Icon = iconFor(refId.kind)
  const kind = registry.get(refId.kind)
  const written = label ?? kind?.chipLabel?.(refId, raw ?? refId.canonical) ?? raw ?? refId.canonical
  // Only replace the inline text if nothing readable was written; keeping `written` prevents
  // layout shift in the source text and hover card when the first fetch resolves.
  const text =
    written || (state.status === "found" && state.entity.chipLabel ? state.entity.chipLabel : refId.canonical)
  return (
    <HoverCard
      open={hoverOpen}
      onOpenChange={(open) => {
        setHoverOpen(open)
        if (open && resolvable) requestRef(refId)
      }}
    >
      <HoverCardTrigger
        delay={240}
        closeDelay={120}
        render={
          <CiteChip
            icon={<Icon aria-hidden="true" />}
            label={clip(text, 56)}
            state={resolvable ? chipState(state.status) : "unsupported"}
            mono={Boolean(mono)}
            data-kind={refId.kind}
            aria-label={`${kindLabel(refId.kind)}: ${text}`}
            onClick={(e) => {
              e.preventDefault()
              e.stopPropagation()
              setHoverOpen(false)
              openCiteDrawer(refId)
            }}
          />
        }
      />
      <HoverCardContent className="w-92" align="start">
        <RefCard refId={refId} onCloseHover={() => setHoverOpen(false)} />
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
