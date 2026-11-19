/**
 * Whether a palette reads: the pairs the theme draws text and marks with, measured on the colours the
 * theme derives (the same mixes surface.css makes), against WCAG's thresholds.
 */
import { contrast, mix } from "./color.ts"
import type { Palette } from "./palette.ts"

export interface ContrastResult {
  pair: string
  ratio: number
  /** What it must reach: 4.5 for text, 3 for large text and marks (borders, rings, icons). */
  min: number
  ok: boolean
}

/** The derived colours the check looks at, as surface.css derives them. */
export function derived(p: Palette) {
  const card = mix(p.background, p.foreground, 0.045)
  return {
    background: p.background,
    card,
    muted: mix(p.background, p.foreground, 0.1),
    mutedForeground: mix(p.foreground, p.background, 0.28),
    primary: p.accent,
    // Button text is the background colour: the rice look, a dark label on a bright accent.
    primaryForeground: p.background,
  }
}

export function checkPalette(p: Palette): ContrastResult[] {
  const d = derived(p)
  const pairs: [string, string, string, number][] = [
    ["text on background", p.foreground, d.background, 4.5],
    ["text on card", p.foreground, d.card, 4.5],
    ["muted text on background", d.mutedForeground, d.background, 4.5],
    ["muted text on card", d.mutedForeground, d.card, 4.5],
    ["button text on primary", d.primaryForeground, d.primary, 4.5],
    ["accent mark on background", p.accent, d.background, 3],
    ["error on card", p.red, d.card, 3],
    ["success on card", p.green, d.card, 3],
    ["warning on card", p.yellow, d.card, 3],
  ]
  return pairs.map(([pair, a, b, min]) => {
    const ratio = Math.round(contrast(a, b) * 100) / 100
    return { pair, ratio, min, ok: ratio >= min }
  })
}
