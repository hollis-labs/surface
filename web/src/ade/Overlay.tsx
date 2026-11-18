/**
 * What the agent is showing the owner, drawn over the desk: the registry's attention layer
 * at the rectangles the hub resolves, and its guide bar at the foot of the content.
 */
import type { Attention, Rect } from "@hollis-labs/ade-core"
import { useEffect, useState, useSyncExternalStore } from "react"
import { AttentionLayer, type AttentionMark } from "@/components/ui/attention-layer"
import { GuideBar } from "@/components/ui/guide-bar"
import { dismissAttention, hub, startAde } from "./hub"

const version = () => hub.getVersion()
const subscribe = (fn: () => void) => hub.subscribe(fn)

/** Redraw a few times a second while anything is shown: windows move without telling anyone. */
function useTick(on: boolean) {
  const [, set] = useState(0)
  useEffect(() => {
    if (!on) return
    const again = () => set((t) => t + 1)
    const timer = window.setInterval(again, 200)
    window.addEventListener("resize", again)
    window.addEventListener("scroll", again, true)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener("resize", again)
      window.removeEventListener("scroll", again, true)
    }
  }, [on])
}

function marksOf(list: Attention[]): AttentionMark[] {
  return list.map((a) => ({
    id: `${a.actor}:${a.style}:${a.seq}`,
    actor: a.actor,
    style: a.style === "cursor" ? "highlight" : a.style,
    rects: a.anchors.map((x) => hub.resolve(x)).filter((r): r is Rect => !!r),
    from: a.from ? hub.resolve(a.from) : null,
    note: a.note,
    by: a.actor === "agent" ? "Agent" : "You",
  }))
}

/** The attention layer, over everything. Mount once. */
export function AdeAttention() {
  useEffect(startAde, [])
  useSyncExternalStore(subscribe, version)
  const list = hub.attentionNow()
  useTick(list.length > 0)
  const spot = list.find((a) => a.style === "spotlight")
  const area = spot ? hub.bounds(spot.anchors[0]?.surface ?? "") : null
  return (
    <AttentionLayer
      marks={marksOf(list)}
      area={area}
      onDismiss={(id) => (id.startsWith("agent:") ? dismissAttention() : hub.clearAttention("user"))}
    />
  )
}

/** The guide bar, at the foot of whatever it is placed in. */
export function AdeGuide() {
  useSyncExternalStore(subscribe, version)
  const g = hub.guideState()
  if (!g || g.status === "stopped") return null
  const step = g.guide.steps[g.index]
  if (!step) return null
  const a = step.await
  const wait =
    g.status === "done" || !a || a.kind === "ack"
      ? undefined
      : a.kind === "state" && typeof a.args?.text === "string"
        ? a.args.text
        : "Do what the step asks."
  return (
    <div className="pointer-events-none absolute inset-x-0 bottom-4 z-40 flex justify-center px-4">
      <GuideBar
        className="pointer-events-auto"
        title={g.guide.title}
        say={step.say}
        step={g.index + 1}
        steps={g.guide.steps.length}
        status={g.status === "done" ? "done" : g.status}
        waiting={wait}
        hint={g.stalled ? `The guide could not do its own part: ${g.stalled.reason}` : wait ? step.hint : undefined}
        onBack={() => hub.guideDo("back")}
        onNext={() => hub.guideDo(wait ? "skip" : "next")}
        onPause={() => hub.guideDo("pause")}
        onResume={() => hub.guideDo("resume")}
        onStop={() => hub.guideDo("stop")}
      />
    </div>
  )
}
