/** ADE's overlays with sample props: the attention layer over two targets, and the guide bar. */
import { useEffect, useRef, useState } from "react"
import { AttentionLayer, type AttentionMark, type Rect } from "@/components/ui/attention-layer"
import { GuideBar } from "@/components/ui/guide-bar"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Section, Specimen } from "./Specimen"

const rectOf = (el: Element | null): Rect | null => {
  if (!el) return null
  const r = el.getBoundingClientRect()
  return r.width && r.height ? { x: r.x, y: r.y, w: r.width, h: r.height } : null
}

/** Rings, a spotlight and an arrow at elements of this window, found again a few times a second. */
function AttentionDemo({ style }: { style: AttentionMark["style"] }) {
  const a = useRef<HTMLDivElement>(null)
  const b = useRef<HTMLDivElement>(null)
  const box = useRef<HTMLDivElement>(null)
  const [, tick] = useState(0)
  const [on, setOn] = useState(true)
  useEffect(() => {
    const t = window.setInterval(() => tick((n) => n + 1), 200)
    return () => window.clearInterval(t)
  }, [])
  const target = rectOf(b.current)
  const from = rectOf(a.current)
  const marks: AttentionMark[] =
    on && target
      ? [{ id: style, actor: "agent", style, rects: [target], from, note: `A ${style}, with a note.`, by: "Agent" }]
      : []
  return (
    <div className="flex w-full flex-col gap-2">
      <Label className="gap-2 text-xs">
        <Switch checked={on} onCheckedChange={setOn} size="sm" /> Show
      </Label>
      <div
        ref={box}
        className="relative grid h-36 w-full grid-cols-2 gap-4 rounded-md border border-dashed border-border p-4"
      >
        <div ref={a} className="grid place-items-center rounded-md bg-muted text-xs">
          from
        </div>
        <div ref={b} className="grid place-items-center rounded-md bg-muted text-xs">
          target
        </div>
      </div>
      <AttentionLayer
        marks={marks}
        area={style === "spotlight" ? rectOf(box.current) : null}
        onDismiss={() => setOn(false)}
      />
    </div>
  )
}

export function Ade() {
  return (
    <>
      <Section title="attention-layer" about="Drawn over the whole window at rectangles the caller finds.">
        {(["highlight", "spotlight", "arrow", "select"] as const).map((s) => (
          <Specimen key={s} label={`style="${s}"`}>
            <AttentionDemo style={s} />
          </Specimen>
        ))}
      </Section>
      <Section title="guide-bar">
        <Specimen label="running, waits for Next">
          <GuideBar
            title="A tour of Drafts"
            say="These are the modes: Review, Edit and Compare."
            step={1}
            steps={3}
            status="running"
            onNext={() => {}}
            onPause={() => {}}
            onStop={() => {}}
          />
        </Specimen>
        <Specimen label="running, waits for something to be done">
          <GuideBar
            title="A tour of Drafts"
            say="Now open Scratch, where quick notes go."
            step={3}
            steps={3}
            status="running"
            waiting="Open Scratch."
            hint="Use the apps menu at the top left."
            onBack={() => {}}
            onNext={() => {}}
            onPause={() => {}}
            onStop={() => {}}
          />
        </Specimen>
        <Specimen label="paused">
          <GuideBar
            title="A tour of Drafts"
            say="This is the draft's text."
            step={2}
            steps={3}
            status="paused"
            onBack={() => {}}
            onResume={() => {}}
            onStop={() => {}}
          />
        </Specimen>
        <Specimen label="done">
          <GuideBar title="A tour of Drafts" say="" step={3} steps={3} status="done" onStop={() => {}} />
        </Specimen>
      </Section>
    </>
  )
}
