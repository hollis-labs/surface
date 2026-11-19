/** Colour arithmetic the theme check needs: hex <-> sRGB, OKLab mixing (what CSS color-mix in oklch
 * does), relative luminance and WCAG contrast. */

export type RGB = [number, number, number]

/** "#rgb", "#rrggbb" or "rrggbb" to sRGB in 0..1; undefined when it is not a colour. */
export function parseHex(s: string): RGB | undefined {
  let h = s.trim().replace(/^#/, "")
  if (/^[0-9a-f]{3}$/i.test(h)) h = [...h].map((c) => c + c).join("")
  if (!/^[0-9a-f]{6}([0-9a-f]{2})?$/i.test(h)) return undefined
  return [0, 2, 4].map((i) => Number.parseInt(h.slice(i, i + 2), 16) / 255) as RGB
}

/** A colour as "#rrggbb" (lower case), or undefined when it is not one. */
export function normalizeHex(s: string | undefined): string | undefined {
  const c = s === undefined ? undefined : parseHex(s)
  return c ? toHex(c) : undefined
}

export function toHex(c: RGB): string {
  return `#${c
    .map((v) =>
      Math.round(Math.min(1, Math.max(0, v)) * 255)
        .toString(16)
        .padStart(2, "0"),
    )
    .join("")}`
}

const lin = (v: number) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4)
const gam = (v: number) => (v <= 0.0031308 ? 12.92 * v : 1.055 * v ** (1 / 2.4) - 0.055)

/** sRGB to OKLab. */
export function toOklab([r, g, b]: RGB): RGB {
  const [R, G, B] = [lin(r), lin(g), lin(b)]
  const l = Math.cbrt(0.4122214708 * R + 0.5363325363 * G + 0.0514459929 * B)
  const m = Math.cbrt(0.2119034982 * R + 0.6806995451 * G + 0.1073969566 * B)
  const s = Math.cbrt(0.0883024619 * R + 0.2817188376 * G + 0.6299787005 * B)
  return [
    0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s,
  ]
}

/** OKLab to sRGB (clamped). */
export function fromOklab([L, a, b]: RGB): RGB {
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3
  const R = 4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s
  const G = -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s
  const B = -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s
  return [R, G, B].map((v) => Math.min(1, Math.max(0, gam(v)))) as RGB
}

/** color-mix(in oklab, a, b weight): weight is b's share, 0..1. Close to the theme's oklch mixes for
 * the near-neutral surfaces it builds, and exact for lightness, which is what contrast depends on. */
export function mix(a: string, b: string, weight: number): string {
  const A = toOklab(parseHex(a) ?? [0, 0, 0])
  const B = toOklab(parseHex(b) ?? [0, 0, 0])
  return toHex(fromOklab(A.map((v, i) => v * (1 - weight) + (B[i] ?? 0) * weight) as RGB))
}

/** WCAG relative luminance. */
export function luminance(hex: string): number {
  const [r, g, b] = (parseHex(hex) ?? [0, 0, 0]).map(lin) as RGB
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

/** WCAG contrast ratio, 1..21. */
export function contrast(a: string, b: string): number {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p) as [number, number]
  return (x + 0.05) / (y + 0.05)
}
