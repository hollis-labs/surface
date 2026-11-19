/**
 * Reading the common Linux theme formats into a Palette:
 * - Omarchy's colors.toml (named keys: background, foreground, accent, red..., or color0..color15),
 * - base16 / tinted-theming schemes (YAML: base00..base0F, flat or under palette:),
 * - pywal / wallust colors.json (special.background/foreground, colors.color0..color15).
 * parseTheme picks the format from the text.
 */
import { luminance, mix, normalizeHex } from "./color.ts"
import type { Palette } from "./palette.ts"

export type ThemeFormat = "omarchy" | "base16" | "pywal"

const slug = (s: string) =>
  s
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "") || "imported"

const modeOf = (bg: string): "dark" | "light" => (luminance(bg) < 0.25 ? "dark" : "light")

function need(v: string | undefined, what: string, format: ThemeFormat): string {
  if (!v) throw new Error(`${format}: no ${what} in the theme`)
  return v
}

/** Omarchy's colors.toml. */
export function parseOmarchy(text: string, name = "Imported"): Palette {
  const kv: Record<string, string> = {}
  for (const m of text.matchAll(/^\s*([a-z0-9_]+)\s*=\s*["']?([^"'\n#]*?#?[0-9a-z]+)["']?\s*(?:#.*)?$/gim)) {
    kv[(m[1] ?? "").toLowerCase()] = m[2] ?? ""
  }
  const c = (...keys: string[]) => {
    for (const k of keys) {
      const v = normalizeHex(kv[k])
      if (v) return v
    }
    return undefined
  }
  const background = need(c("background", "color0"), "background", "omarchy")
  const foreground = need(c("foreground", "color7", "color15"), "foreground", "omarchy")
  const blue = need(c("blue", "color4"), "blue", "omarchy")
  return {
    id: slug(name),
    name,
    mode: kv.mode === "light" ? "light" : kv.mode === "dark" ? "dark" : modeOf(background),
    source: "Omarchy colors.toml",
    background,
    foreground,
    muted: c("muted", "dark_foreground", "color8"),
    accent: c("accent") ?? blue,
    selection: c("selection", "selection_background"),
    red: need(c("red", "color1"), "red", "omarchy"),
    green: need(c("green", "color2"), "green", "omarchy"),
    yellow: need(c("yellow", "color3"), "yellow", "omarchy"),
    blue,
    magenta: need(c("magenta", "color5"), "magenta", "omarchy"),
    cyan: need(c("cyan", "color6"), "cyan", "omarchy"),
    orange: c("orange"),
  }
}

/** A base16 / tinted-theming scheme. */
export function parseBase16(text: string, fallbackName = "Imported"): Palette {
  const b: Record<string, string> = {}
  for (const m of text.matchAll(/\bbase0([0-9a-f])\s*:\s*["']?#?([0-9a-f]{6})["']?/gi)) {
    b[(m[1] ?? "").toUpperCase()] = `#${(m[2] ?? "").toLowerCase()}`
  }
  const name = text.match(/^\s*(?:name|scheme)\s*:\s*["']?([^"'\n]+?)["']?\s*$/im)?.[1] ?? fallbackName
  const variant = text.match(/^\s*variant\s*:\s*["']?(dark|light)["']?\s*$/im)?.[1] as "dark" | "light" | undefined
  const g = (k: string) => need(b[k], `base0${k}`, "base16")
  const background = g("0")
  return {
    id: slug(name),
    name,
    mode: variant ?? modeOf(background),
    source: "base16",
    background,
    foreground: g("5"),
    muted: g("3"),
    accent: g("D"),
    selection: g("2"),
    red: g("8"),
    orange: b["9"],
    yellow: g("A"),
    green: g("B"),
    cyan: g("C"),
    blue: g("D"),
    magenta: g("E"),
  }
}

/** pywal's (or wallust's) colors.json. */
export function parsePywal(text: string, name = "Imported"): Palette {
  const j = JSON.parse(text) as {
    special?: { background?: string; foreground?: string; cursor?: string }
    colors?: Record<string, string>
  }
  const col = (n: number) => normalizeHex(j.colors?.[`color${n}`])
  const background = need(normalizeHex(j.special?.background) ?? col(0), "background", "pywal")
  const foreground = need(normalizeHex(j.special?.foreground) ?? col(7), "foreground", "pywal")
  const blue = need(col(4), "color4", "pywal")
  return {
    id: slug(name),
    name,
    mode: modeOf(background),
    source: "pywal",
    background,
    foreground,
    muted: col(8) ?? mix(foreground, background, 0.45),
    accent: blue,
    red: need(col(1), "color1", "pywal"),
    green: need(col(2), "color2", "pywal"),
    yellow: need(col(3), "color3", "pywal"),
    blue,
    magenta: need(col(5), "color5", "pywal"),
    cyan: need(col(6), "color6", "pywal"),
  }
}

/** Which format a theme file is in. */
export function detectFormat(text: string): ThemeFormat {
  const t = text.trim()
  if (t.startsWith("{")) return "pywal"
  if (/\bbase0[0-9a-f]\s*:/i.test(t)) return "base16"
  return "omarchy"
}

/** Any supported theme file to a Palette. */
export function parseTheme(text: string, name?: string): Palette {
  switch (detectFormat(text)) {
    case "pywal":
      return parsePywal(text, name)
    case "base16":
      return parseBase16(text, name)
    default:
      return parseOmarchy(text, name)
  }
}
