/**
 * The bar's theme picker and clock. The palette and the type style are the app's live settings
 * (surface.palette, surface.type): choosing one here redraws the whole app at once, and it is
 * remembered for the app.
 */
import { DEFAULT_PALETTE, PALETTES, paletteById, type TypeStyle } from "@hollis-labs/surface-theme"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useSetting } from "../sdk"
import { setSetting } from "../sdk/runtime"

/** A few of a palette's colours, as dots, by role. */
function Dots({ colors }: { colors: Record<string, string> }) {
  return (
    <span className="flex shrink-0 items-center gap-0.5" aria-hidden>
      {Object.entries(colors).map(([role, c]) => (
        <span key={role} className="size-2 rounded-full" style={{ background: c }} />
      ))}
    </span>
  )
}

export function ThemePicker() {
  const id = (useSetting<string>("surface.palette") ?? DEFAULT_PALETTE.id) as string
  const type = ((useSetting<string>("surface.type") ?? "mixed") === "mono" ? "mono" : "mixed") as TypeStyle
  const palette = paletteById(id) ?? DEFAULT_PALETTE
  const set = (key: string, value: string) => void setSetting(key, value).catch(() => {})
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="xs"
            className="h-5.5 gap-1.5 rounded-sm px-2 font-mono text-xs text-muted-foreground hover:bg-accent hover:text-foreground"
            title="Theme"
            aria-label={`Theme: ${palette.name}`}
          >
            <Dots
              colors={{
                accent: palette.accent,
                accent2: palette.accent2 ?? palette.cyan,
                red: palette.red,
                green: palette.green,
              }}
            />
            <span>{palette.name}</span>
          </Button>
        }
      />
      <DropdownMenuContent align="end" className="min-w-60">
        <DropdownMenuGroup>
          <DropdownMenuLabel>Palette</DropdownMenuLabel>
          <DropdownMenuRadioGroup value={palette.id} onValueChange={(v) => set("surface.palette", String(v))}>
            {PALETTES.map((p) => (
              <DropdownMenuRadioItem key={p.id} value={p.id}>
                <Dots
                  colors={{ background: p.background, accent: p.accent, accent2: p.accent2 ?? p.cyan, red: p.red }}
                />
                <span>{p.name}</span>
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuLabel>Type</DropdownMenuLabel>
          <DropdownMenuRadioGroup value={type} onValueChange={(v) => set("surface.type", String(v))}>
            <DropdownMenuRadioItem value="mixed">Mixed (mono chrome)</DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="mono">Mono everywhere</DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** The date and time, to the minute, matching Basalisk's status bar clock. */
export function Clock() {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 15_000)
    return () => clearInterval(t)
  }, [])
  const date = now.toLocaleDateString([], { weekday: "short", day: "numeric", month: "short" })
  const time = now.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
  return (
    <time
      className="flex items-center gap-2.5 pr-1 pl-2 font-mono text-xs tabular-nums text-foreground"
      dateTime={now.toISOString()}
      title={now.toLocaleString()}
    >
      <span className="text-muted-foreground">{date}</span>
      {time}
    </time>
  )
}
