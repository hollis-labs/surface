import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { test } from "node:test"
import { checkPalette, contrast, detectFormat, mix, PALETTES, paletteById, paletteCSS, parseTheme } from "./index.ts"

const fixture = (f: string) => readFileSync(new URL(`../test/${f}`, import.meta.url), "utf8")

test("Omarchy colors.toml, named keys", () => {
  const p = parseTheme(fixture("tokyo-night.colors.toml"), "Tokyo Night")
  assert.equal(detectFormat(fixture("tokyo-night.colors.toml")), "omarchy")
  assert.deepEqual(
    [p.id, p.mode, p.background, p.foreground, p.accent, p.red, p.cyan, p.orange],
    ["tokyo-night", "dark", "#1a1b26", "#a9b1d6", "#7aa2f7", "#f7768e", "#449dab", "#eb927b"],
  )
})

test("Omarchy colors.toml, the older color0..color15", () => {
  const p = parseTheme(fixture("legacy.colors.toml"), "Gruvbox old")
  assert.deepEqual(
    [p.background, p.red, p.blue, p.accent, p.muted],
    ["#1d2021", "#ea6962", "#7daea3", "#7daea3", "#3c3836"],
  )
})

test("base16 / tinted-theming", () => {
  const text = fixture("dracula.base16.yaml")
  assert.equal(detectFormat(text), "base16")
  const p = parseTheme(text)
  assert.deepEqual(
    [p.id, p.name, p.mode, p.background, p.foreground, p.selection, p.red, p.orange, p.magenta],
    ["dracula", "Dracula", "dark", "#282a36", "#f8f8f2", "#44475a", "#ff5555", "#ffb86c", "#ff79c6"],
  )
})

test("pywal colors.json", () => {
  const text = fixture("wal.colors.json")
  assert.equal(detectFormat(text), "pywal")
  const p = parseTheme(text, "City")
  assert.deepEqual(
    [p.background, p.foreground, p.accent, p.muted, p.mode],
    ["#0b0d10", "#c3c7cb", "#4f6d8a", "#5c6166", "dark"],
  )
})

test("a theme missing a colour is refused, saying which", () => {
  assert.throws(() => parseTheme('background = "#000000"\nforeground = "#ffffff"'), /no blue/)
  assert.throws(() => parseTheme("base00: '#000000'"), /no base05/)
})

test("colour arithmetic", () => {
  assert.equal(Math.round(contrast("#000000", "#ffffff")), 21)
  assert.equal(mix("#000000", "#ffffff", 0), "#000000")
  assert.equal(mix("#000000", "#ffffff", 1), "#ffffff")
})

test("the shipped palettes: Blade Runner first, every field a colour, a CSS property per field", () => {
  assert.equal(PALETTES[0]?.id, "blade-runner")
  assert.ok(paletteById("tokyo-night"))
  for (const p of PALETTES) {
    for (const k of [
      "background",
      "foreground",
      "accent",
      "red",
      "green",
      "yellow",
      "blue",
      "magenta",
      "cyan",
    ] as const) {
      assert.match(p[k], /^#[0-9a-f]{6}$/, `${p.id}.${k}`)
    }
    assert.equal(paletteCSS(p)["--p-bg"], p.background)
  }
})

test("every shipped palette's body text reads (4.5:1 on background and card)", () => {
  for (const p of PALETTES) {
    for (const r of checkPalette(p).filter((x) => x.pair.startsWith("text on"))) {
      assert.ok(r.ok, `${p.id}: ${r.pair} ${r.ratio}`)
    }
  }
})

test("a palette's missing colours come from its own, never the default's", () => {
  const p = parseTheme(fixture("wal.colors.json"), "City")
  const css = paletteCSS(p)
  assert.equal(css["--p-accent-2"], p.cyan)
  assert.match(css["--p-selection"] ?? "", /^#[0-9a-f]{6}$/)
  assert.match(css["--p-orange"] ?? "", /^#[0-9a-f]{6}$/)
  assert.equal(Object.keys(css).length, 13)
})
