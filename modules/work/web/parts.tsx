/** Pieces the Work views share: kind chips, the kind icon, a labelled choice, the new-item menu, a card. */
import { Icon } from "@hollis-labs/surface-page/sdk"
import type { ReactNode } from "react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Markdown } from "@/components/ui/markdown"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import type { WorkCard, WorkKind } from "./api"
import { ago, DRAG_TYPE } from "./util"

/** A card's excerpt is one line of its body: drawn as inline Markdown, its links as plain text (the card is the click). */
const excerptParts: NonNullable<Parameters<typeof Markdown>[0]["components"]> = {
  p: ({ node: _n, ...p }) => <span {...p} />,
  a: ({ node: _n, href: _h, ...p }) => <span {...p} />,
}

/** Kind icon names the presets use that lucide has since renamed. */
// Kinds name icons by older lucide names; these are their lucide 1.x names.
const ICON_ALIAS: Record<string, string> = {
  "alert-triangle": "triangle-exclamation-point",
  "triangle-alert": "triangle-exclamation-point",
  "check-circle-2": "circle-check",
}

/** A kind's icon in its colour. */
export function KindIcon({ kind, size = 12 }: { kind?: WorkKind; size?: number }) {
  return (
    <span data-work-color={kind?.color} className="inline-flex shrink-0">
      <Icon name={ICON_ALIAS[kind?.icon ?? ""] ?? (kind?.icon || "circle")} size={size} className="work-kind" />
    </span>
  )
}

/** The kind chips a view shows above itself: which kinds it shows, kept in the view item. */
export function KindChips({ kinds, shown, set }: { kinds: WorkKind[]; shown: string[]; set: (k: string[]) => void }) {
  return (
    <ToggleGroup
      multiple
      size="sm"
      spacing={0}
      aria-label="Kinds shown"
      className="flex-wrap"
      value={shown}
      onValueChange={(v: string[]) => {
        if (v.length === 0) return
        set(kinds.map((k) => k.kind).filter((k) => v.includes(k)))
      }}
    >
      {kinds.map((k) => (
        <ToggleGroupItem
          key={k.kind}
          value={k.kind}
          title={shown.includes(k.kind) ? `Hide ${k.plural || k.label}` : `Show ${k.plural || k.label}`}
        >
          <KindIcon kind={k} />
          {k.plural || k.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

/** A choice from a short list, on the registry's Select. */
export function Pick({
  label,
  value,
  options,
  onChange,
  className,
}: {
  label: string
  value: string
  options: { value: string; label: string }[]
  onChange: (v: string) => void
  className?: string
}) {
  const opts = options.some((o) => o.value === value) ? options : [{ value, label: value || "—" }, ...options]
  return (
    <Select<string> value={value} onValueChange={(v) => onChange(String(v ?? ""))} items={opts}>
      <SelectTrigger size="sm" aria-label={label} title={label} className={className}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {opts.map((o) => (
          <SelectItem key={o.value || "\u0000none"} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

/** A menu of kinds to make a new item of. */
export function MakeMenu({
  kinds,
  make,
  label,
  title = "New item",
}: {
  kinds: WorkKind[]
  make: (kind: string) => void
  label?: string
  title?: string
}) {
  if (kinds.length === 0) return null
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant={label ? "outline" : "ghost"}
            size={label ? "sm" : "icon-xs"}
            title={title}
            aria-label={title}
          >
            <Icon name="plus" />
            {label}
          </Button>
        }
      />
      <DropdownMenuContent align="start" className="w-48">
        <DropdownMenuLabel>{title}</DropdownMenuLabel>
        {kinds.map((k) => (
          <DropdownMenuItem key={k.kind} onClick={() => make(k.kind)}>
            <KindIcon kind={k} />
            {k.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** One item as a card: title, excerpt and what it is, draggable by the browser's drag and drop. */
export function ItemCard({
  card,
  kind,
  picked,
  pick,
  draggable = true,
  meta,
  compact,
}: {
  card: WorkCard
  kind?: WorkKind
  picked: boolean
  pick: (id: string, newWindow?: boolean) => void
  draggable?: boolean
  meta?: ReactNode
  compact?: boolean
}) {
  const date = card.fields.publish_on ?? card.fields.due
  return (
    <div
      data-work-color={kind?.color}
      data-picked={picked}
      className="work-card group flex cursor-pointer flex-col gap-1 rounded-md border bg-card px-2 py-1.5 text-sm hover:bg-accent"
      draggable={draggable}
      onDragStart={(e) => {
        e.dataTransfer.setData(DRAG_TYPE, card.id)
        e.dataTransfer.effectAllowed = "move"
      }}
      onClick={(e) => pick(card.id, e.metaKey || e.ctrlKey || e.shiftKey)}
      onAuxClick={(e) => {
        if (e.button === 1) {
          e.preventDefault()
          pick(card.id, true)
        }
      }}
      title={`${kind?.label ?? card.kind} ${card.id} (⌘-click to open in new window)`}
    >
      <span className="flex min-w-0 items-center gap-1.5 font-medium">
        <KindIcon kind={kind} />
        <span className="truncate">{card.title || "Untitled"}</span>
        {card.signed && (
          <Icon
            name="badge-check"
            size={12}
            className={card.stale_sign ? "shrink-0 text-warning" : "shrink-0 text-success"}
            aria-label={card.stale_sign ? "Signed off; changed since" : "Signed off"}
          />
        )}
        <Button
          variant="ghost"
          size="icon-xs"
          className="ml-auto shrink-0 opacity-0 transition-opacity group-hover:opacity-100"
          title="Open in new window"
          aria-label={`Open ${card.title || "Untitled"} in new window`}
          onClick={(e) => {
            e.stopPropagation()
            pick(card.id, true)
          }}
        >
          <Icon name="external-link" />
        </Button>
      </span>
      {!compact &&
        (card.problem ? (
          <span className="text-warning text-xs">{card.problem}</span>
        ) : (
          card.excerpt && (
            <Markdown className="line-clamp-2 text-muted-foreground text-xs" components={excerptParts}>
              {card.excerpt}
            </Markdown>
          )
        ))}
      {!compact && (
        <span className="text-muted-foreground text-xs">
          {meta ?? (
            <>
              {date ? `${String(date)} · ` : ""}
              {card.words > 0 ? `${card.words} words · ` : ""}
              {card.children.length > 0 ? `${card.children.length} inside · ` : ""}
              {ago(card.updated)}
            </>
          )}
        </span>
      )}
    </div>
  )
}

/** A bar above a view: its filters and tools. */
export function Bar({ children }: { children: ReactNode }) {
  return <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-2 py-1.5">{children}</div>
}

/** A titled part of the detail. */
export function Sec({ title, children, actions }: { title: ReactNode; children: ReactNode; actions?: ReactNode }) {
  return (
    <section className="flex flex-col gap-1.5 border-t px-3 py-2.5">
      <h4 className="flex items-center gap-1.5 font-medium text-muted-foreground text-xs uppercase tracking-wide">
        {title}
        {actions && <span className="ml-auto flex items-center gap-1 normal-case tracking-normal">{actions}</span>}
      </h4>
      {children}
    </section>
  )
}
