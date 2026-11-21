/**
 * The bar's attention badge (docs/attention.md): how many things wait for the owner. Highlighted
 * when one needs an answer; a click (or cmd+.) brings the first one in front. Nothing shows when
 * nothing waits.
 */
import { Button } from "@/components/ui/button"
import { Icon } from "../sdk"
import { useAttention } from "./attention"
import { jumpTo, type UI } from "./commands"

const words: Record<string, string> = { needs_you: "need you", failed: "failed", unseen: "not seen yet" }

export function AttentionBadge({ ui }: { ui: UI }) {
  const a = useAttention()
  const total = a.items.length
  if (total === 0) return null
  const needs = a.counts.needs_you ?? a.items.filter((i) => i.state === "needs_you").length
  const byState: Record<string, number> = {}
  for (const i of a.items) byState[i.state] = (byState[i.state] ?? 0) + 1
  const summary = Object.entries(byState)
    .map(([state, n]) => `${n} ${words[state] ?? state}`)
    .join(" · ")
  const first = a.items[0]
  return (
    <Button
      variant="ghost"
      size="xs"
      className={`h-5.5 gap-1.5 rounded-sm px-2 font-mono text-xs ${
        needs > 0
          ? "bg-accent text-primary hover:bg-accent hover:text-foreground"
          : "text-muted-foreground hover:bg-accent hover:text-foreground"
      }`}
      aria-pressed={needs > 0}
      data-bar-on={needs > 0 || undefined}
      aria-label={`${total} waiting: ${summary}. Jump to the first.`}
      title={`${summary}\nNext: ${first?.title ?? first?.subject ?? ""}  (⌘.)`}
      onClick={() => void jumpTo(first, ui)}
    >
      <Icon name={needs > 0 ? "bell-ring" : "bell"} size={14} />
      <span className="tabular-nums">{needs > 0 ? `${needs} for you` : `${total}`}</span>
    </Button>
  )
}
