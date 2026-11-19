# @hollis-labs/surface-theme

The palettes behind the Surface theme. A palette is what a Linux theme says about colour, in one shape:
background, foreground, accent, selection and the terminal's red, green, yellow, blue, magenta and cyan.
The theme (`registry/theme/surface.css`) derives every UI token from those, in the browser, so a palette
is all a theme needs.

```ts
import { applyPalette, checkPalette, PALETTES, paletteById, parseTheme } from "@hollis-labs/surface-theme"

applyPalette(paletteById("tokyo-night")!)            // the page follows: surfaces, borders, primary, states
applyPalette(parseTheme(text, "Mine"), el, "mono")   // an Omarchy colors.toml, base16 YAML or pywal JSON
checkPalette(p)                                      // WCAG contrast of the pairs the theme draws with
```

- **Shipped:** Blade Runner (the default), Aura, Moodpeak and Cyberphunk (Basalisk's), and Omarchy's
  Tokyo Night, Catppuccin, Gruvbox, Nord, Kanagawa, Everforest, Matte Black, Hackerman and Retro 82.
- **Read:** Omarchy `colors.toml` (named keys or `color0`..`color15`), base16 / tinted-theming schemes
  (`base00`..`base0F`), pywal / wallust `colors.json`.
- **Type:** `applyPalette(p, el, "mono")` sets everything in the mono font; `"mixed"` keeps mono chrome over
  a sans body.
