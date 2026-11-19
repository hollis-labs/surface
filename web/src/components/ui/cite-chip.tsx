import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "cn"

/**
 * A reference inline in text: an icon and a short label, set like a word in the sentence.
 * What it says and what happens on hover or click are the caller's (Surface's page core,
 * web/src/cite); the chip only draws the state it is given. Pass it as a hover card's trigger.
 */
const citeChipVariants = cva(
  "mx-px inline-flex max-w-full translate-y-[-1px] cursor-pointer items-center gap-1 rounded-sm border px-1 py-px align-baseline text-[0.9em] leading-tight font-medium whitespace-nowrap transition-colors outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 [&>svg]:size-3 [&>svg]:shrink-0",
  {
    variants: {
      state: {
        idle: "border-border bg-muted/50 text-foreground hover:bg-muted",
        loading: "border-border bg-muted/50 text-muted-foreground hover:bg-muted",
        found: "border-primary/25 bg-primary/10 text-foreground hover:bg-primary/15",
        "not-found": "border-dashed border-border bg-transparent text-muted-foreground line-through decoration-muted-foreground/50 hover:bg-muted/50",
        error: "border-destructive/30 bg-destructive/5 text-foreground hover:bg-destructive/10",
        unsupported: "border-border bg-transparent text-foreground hover:bg-muted/50",
      },
      mono: {
        true: "font-mono text-[0.85em] font-normal",
        false: "",
      },
    },
    defaultVariants: { state: "idle", mono: false },
  }
)

type CiteChipState = NonNullable<VariantProps<typeof citeChipVariants>["state"]>

function CiteChip({
  className,
  state = "idle",
  mono = false,
  icon,
  label,
  ...props
}: Omit<React.ComponentProps<"button">, "children"> &
  VariantProps<typeof citeChipVariants> & {
    /** A lucide icon element, drawn small before the label. */
    icon?: React.ReactNode
    label: string
  }) {
  return (
    <button
      type="button"
      data-slot="cite-chip"
      data-state={state}
      className={cn(citeChipVariants({ state, mono }), className)}
      {...props}
    >
      {icon}
      <span className="min-w-0 truncate">{label}</span>
    </button>
  )
}

export { CiteChip, citeChipVariants, type CiteChipState }
