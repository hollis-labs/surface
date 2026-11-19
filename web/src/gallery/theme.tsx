/** Theme: the palette in use, the tokens derived from it, how it reads, every shipped palette side by
 * side, the two type styles, and reading a theme file. Design job d_97410e09, round 1. */
import {
  applyPalette,
  checkPalette,
  PALETTES,
  type Palette,
  paletteCSS,
  parseTheme,
  type TypeStyle,
} from "@hollis-labs/surface-theme"
import { createContext, type ReactNode, useContext, useLayoutEffect, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Section, Specimen } from "./Specimen"

const JOB = "d_97410e09"

/** The gallery's palette and type style, chosen in its header. */
export const ThemeChoice = createContext<{
  palette: Palette
  type: TypeStyle
  setPalette: (p: Palette) => void
  setType: (t: TypeStyle) => void
}>({ palette: PALETTES[0] as Palette, type: "mixed", setPalette: () => {}, setType: () => {} })

/** Draws its children in a palette (and type style) of their own. */
export function InPalette({
  palette,
  type,
  children,
  className,
}: {
  palette: Palette
  type?: TypeStyle
  children: ReactNode
  className?: string
}) {
  const ref = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    if (!ref.current) return
    const undo = applyPalette(palette, ref.current, type ?? "mixed")
    return undo
  }, [palette, type])
  return (
    <div ref={ref} className={`bg-background text-foreground ${className ?? ""}`}>
      {children}
    </div>
  )
}

const TOKENS = [
  "desk",
  "background",
  "card",
  "popover",
  "muted",
  "secondary",
  "accent",
  "border",
  "input",
  "primary",
  "ring",
  "destructive",
  "success",
  "warning",
  "info",
  "selection",
] as const

function Swatch({ color, label, detail }: { color: string; label: string; detail?: string }) {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <span className="size-7 shrink-0 rounded-md border" style={{ background: color }} />
      <span className="flex min-w-0 flex-col">
        <span className="truncate text-xs">{label}</span>
        {detail && <span className="truncate font-mono text-[10px] text-muted-foreground">{detail}</span>}
      </span>
    </div>
  )
}

/** A small piece of interface: enough to judge a palette at a glance. */
function Sample({ palette }: { palette: Palette }) {
  return (
    <Card size="sm" className="w-full">
      <CardHeader>
        <CardTitle>{palette.name}</CardTitle>
        <CardDescription>{palette.source ?? "palette"}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 text-sm">
        <p>The agent read three files and wrote a draft.</p>
        <p className="text-muted-foreground">Muted: updated 2 min ago · 14 notes</p>
        <div className="flex flex-wrap gap-1.5">
          <Badge variant="outline" className="text-success">
            done
          </Badge>
          <Badge variant="outline" className="text-warning">
            waiting
          </Badge>
          <Badge variant="outline" className="text-destructive">
            failed
          </Badge>
          <Badge variant="outline" className="text-info">
            info
          </Badge>
        </div>
        <div className="flex gap-1">
          {(["red", "green", "yellow", "blue", "magenta", "cyan"] as const).map((k) => (
            <span key={k} className="h-1.5 flex-1 rounded-full" style={{ background: palette[k] }} />
          ))}
        </div>
      </CardContent>
      <CardFooter className="justify-end gap-2">
        <Button size="sm" variant="outline">
          Cancel
        </Button>
        <Button size="sm">Run</Button>
      </CardFooter>
    </Card>
  )
}

function Checks({ palette }: { palette: Palette }) {
  return (
    <ul className="flex w-full flex-col gap-1 text-xs">
      {checkPalette(palette).map((r) => (
        <li key={r.pair} className="flex items-center justify-between gap-2">
          <span>{r.pair}</span>
          <span className={r.ok ? "text-success" : "text-destructive"}>
            {r.ratio.toFixed(2)} {r.ok ? "≥" : "<"} {r.min}
          </span>
        </li>
      ))}
    </ul>
  )
}

export function Theme() {
  const { palette, setPalette } = useContext(ThemeChoice)
  return (
    <>
      <Section
        title="Theme: the palette in use"
        about="Thirteen colours a Linux theme names. Everything else is derived from them by the theme. Pick another in the header."
        draft={JOB}
      >
        <Specimen label={`palette: ${palette.name}`} wide>
          <div className="grid w-full grid-cols-2 gap-3 @lg:grid-cols-4 @3xl:grid-cols-7">
            {Object.entries(paletteCSS(palette)).map(([name, v]) => (
              <Swatch key={name} color={v} label={name.replace("--p-", "")} detail={v} />
            ))}
          </div>
        </Specimen>
        <Specimen label="derived tokens" wide>
          <div className="grid w-full grid-cols-2 gap-3 @lg:grid-cols-4 @3xl:grid-cols-8">
            {TOKENS.map((t) => (
              <Swatch key={t} color={`var(--${t})`} label={t} />
            ))}
          </div>
        </Specimen>
        <Specimen label="how it reads (WCAG contrast)">
          <Checks palette={palette} />
        </Specimen>
        <Specimen label="a piece of interface">
          <Sample palette={palette} />
        </Specimen>
      </Section>

      <Section
        title="Theme: every palette"
        about="Each drawn in its own palette. Choose one to use it across the gallery."
        draft={JOB}
      >
        {PALETTES.map((p) => {
          const fails = checkPalette(p).filter((r) => !r.ok).length
          return (
            <Specimen key={p.id} label={`${p.id}${fails ? ` · ${fails} contrast warning(s)` : ""}`}>
              <InPalette palette={p} className="flex w-full flex-col gap-2 rounded-lg p-2">
                <Sample palette={p} />
                <Button size="xs" variant={p.id === palette.id ? "secondary" : "ghost"} onClick={() => setPalette(p)}>
                  {p.id === palette.id ? "In use" : "Use this palette"}
                </Button>
              </InPalette>
            </Specimen>
          )
        })}
      </Section>

      <Section
        title="Theme: type"
        about="Mono chrome (bars, buttons, tabs, badges, titles) over a sans body, or mono everywhere. Switch it for the whole gallery in the header."
        draft={JOB}
      >
        {(["mixed", "mono"] as const).map((t) => (
          <Specimen
            key={t}
            label={t === "mixed" ? "mixed: mono chrome, sans body" : "mono: everything in the mono font"}
          >
            <InPalette palette={palette} type={t} className="w-full rounded-lg p-2">
              <Sample palette={palette} />
            </InPalette>
          </Specimen>
        ))}
      </Section>

      <Section
        title="Theme: read a theme file"
        about="Paste an Omarchy colors.toml, a base16 / tinted-theming scheme or a pywal colors.json."
        draft={JOB}
      >
        <ImportSpecimen onUse={setPalette} />
      </Section>
    </>
  )
}

function ImportSpecimen({ onUse }: { onUse: (p: Palette) => void }) {
  const [text, setText] = useState("")
  const [name, setName] = useState("")
  const [read, setRead] = useState<Palette | null>(null)
  const [error, setError] = useState("")
  const preview = () => {
    try {
      setRead(parseTheme(text, name.trim() || undefined))
      setError("")
    } catch (e) {
      setRead(null)
      setError((e as Error).message)
    }
  }
  return (
    <>
      <Specimen label="the file">
        <div className="flex w-full flex-col gap-2">
          <Input placeholder="Name (optional)" value={name} onChange={(e) => setName(e.target.value)} />
          <Textarea
            className="min-h-40 font-mono text-xs"
            placeholder={'background = "#1a1b26"\nforeground = "#a9b1d6"\naccent = "#7aa2f7"\n…'}
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
          <div className="flex items-center justify-end gap-2">
            {error && <span className="mr-auto text-xs text-destructive">{error}</span>}
            <Button size="sm" variant="outline" disabled={!text.trim()} onClick={preview}>
              Read it
            </Button>
            <Button size="sm" disabled={!read} onClick={() => read && onUse(read)}>
              Use it
            </Button>
          </div>
        </div>
      </Specimen>
      <Specimen label={read ? `read as ${read.source}: ${read.name}` : "nothing read yet"}>
        {read ? (
          <InPalette palette={read} className="flex w-full flex-col gap-2 rounded-lg p-2">
            <Sample palette={read} />
            <Checks palette={read} />
          </InPalette>
        ) : (
          <p className="text-xs text-muted-foreground">Paste a theme and read it to see it here.</p>
        )}
      </Specimen>
    </>
  )
}
