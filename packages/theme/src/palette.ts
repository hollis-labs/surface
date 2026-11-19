/**
 * A palette: what a Linux theme says about colour, in one shape. Every theme format reduces to this
 * (Omarchy's colors.toml, base16 / tinted-theming, pywal / wallust, a terminal's 16 colours), and
 * the Surface theme derives every UI token from it in CSS: surfaces, borders, primary, ring,
 * success / warning / error / info, charts.
 */
export interface Palette {
  id: string
  name: string
  mode: "dark" | "light"
  /** Where it came from, in a few words. */
  source?: string
  background: string
  foreground: string
  /** Quiet text and lines; derived from foreground and background when absent. */
  muted?: string
  /** The one colour that says "this": primary buttons, the focus ring, selection marks. */
  accent: string
  /** A second accent for highlights and the far end of a gradient border; cyan when absent. */
  accent2?: string
  /** Selected text and selected rows. */
  selection?: string
  red: string
  green: string
  yellow: string
  blue: string
  magenta: string
  cyan: string
  orange?: string
}

/** The CSS custom properties a palette sets (the theme's --p-* inputs), by palette field. */
export const PALETTE_VARS: Record<Exclude<keyof Palette, "id" | "name" | "mode" | "source">, string> = {
  background: "--p-bg",
  foreground: "--p-fg",
  muted: "--p-muted",
  accent: "--p-accent",
  accent2: "--p-accent-2",
  selection: "--p-selection",
  red: "--p-red",
  green: "--p-green",
  yellow: "--p-yellow",
  blue: "--p-blue",
  magenta: "--p-magenta",
  cyan: "--p-cyan",
  orange: "--p-orange",
}

/** The CSS a palette is: its --p-* properties, for a stylesheet or an element's style. */
export function paletteCSS(p: Palette): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [field, name] of Object.entries(PALETTE_VARS)) {
    const v = p[field as keyof typeof PALETTE_VARS]
    if (v) out[name] = v
  }
  return out
}

/** The type style: mono chrome over sans body ("mixed"), or mono everywhere ("mono"). */
export type TypeStyle = "mixed" | "mono"

/**
 * Applies a palette (and, when given, a type style) to an element, the page's root by default. Fields
 * the palette leaves out fall back to the theme's derivation. Returns a function that removes them.
 */
export function applyPalette(p: Palette, el: HTMLElement = document.documentElement, type?: TypeStyle): () => void {
  const css = paletteCSS(p)
  for (const name of Object.values(PALETTE_VARS)) el.style.removeProperty(name)
  for (const [name, v] of Object.entries(css)) el.style.setProperty(name, v)
  el.dataset.palette = p.id
  el.classList.toggle("dark", p.mode === "dark")
  el.style.colorScheme = p.mode
  if (type) el.dataset.type = type
  return () => {
    for (const name of Object.values(PALETTE_VARS)) el.style.removeProperty(name)
    delete el.dataset.palette
  }
}
