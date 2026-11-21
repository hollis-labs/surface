import { type ModuleWeb, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"

interface Brief {
  id: string
  title: string
  outcome: string
  user_steps: string
  agent_steps: string
  system_steps: string
  acceptance: string
  stage: "draft" | "review" | "ready"
  revision: number
  updated: string
}
const blank: Brief = {
  id: "",
  title: "",
  outcome: "",
  user_steps: "",
  agent_steps: "",
  system_steps: "",
  acceptance: "",
  stage: "draft",
  revision: 0,
  updated: "",
}
const fields = [
  ["outcome", "Intended outcome", "What should be true when the workflow succeeds?"],
  ["user_steps", "User actions", "Decisions, review points, and what the person does next."],
  ["agent_steps", "Agent actions", "What the agent reads, proposes, changes, and hands back."],
  ["system_steps", "System behavior", "State transitions, events, persistence, and recovery."],
  ["acceptance", "Acceptance checks", "A walkthrough, failure cases, and evidence of completion."],
] as const

function Workflows({ win, desk, api }: ViewProps) {
  const [briefs, setBriefs] = useState<Brief[]>([])
  const [draft, setDraft] = useState<Brief>(blank)
  const [dirty, setDirty] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [reviewText, setReviewText] = useState("")
  const [copied, setCopied] = useState(false)
  const load = useCallback(() => {
    api
      .get<Brief[]>("briefs")
      .then(setBriefs)
      .catch((e: Error) => setError(e.message))
  }, [api])
  useEffect(load, [load])
  useEvent("workflows.changed", load)
  useEffect(() => {
    desk.report(win.id, { kind: "workflow", ref: `workflows:${draft.id}`, brief: draft.id })
  }, [desk, win.id, draft.id])
  useEffect(() => {
    desk.setDirty(win.id, dirty)
    return () => desk.setDirty(win.id, false)
  }, [desk, win.id, dirty])
  const latest = briefs.find((b) => b.id === draft.id)
  const stale = !!latest && latest.revision !== draft.revision
  const select = async (b: Brief) => {
    if (dirty && !(await desk.confirm({ title: "Discard unsaved workflow changes?" }))) return
    setDraft(b)
    setDirty(false)
    setReviewText("")
    setCopied(false)
    setError("")
  }
  const edit = (key: keyof Brief, value: string) => {
    setDraft((b) => ({ ...b, [key]: value, stage: "draft" }))
    setDirty(true)
    setReviewText("")
    setCopied(false)
  }
  const save = async (stage: Brief["stage"]) => {
    setBusy(true)
    setError("")
    try {
      const b = await api.post<Brief>("briefs", { ...draft, stage })
      setDraft(b)
      setDirty(false)
      setReviewText("")
      setCopied(false)
      load()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const prepare = async () => {
    setBusy(true)
    setError("")
    setCopied(false)
    try {
      const result = await api.get<{ text: string }>(`briefs/${draft.id}/handoff`)
      setReviewText(result.text)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="flex h-full min-h-0 flex-col gap-3 overflow-auto p-3">
      <header className="flex items-center justify-between gap-2">
        <div>
          <h2 className="font-semibold">Workflow briefs</h2>
          <p className="text-muted-foreground text-sm">Design how people, agents and systems work together.</p>
        </div>
        <Button variant="outline" disabled={busy} onClick={() => void select(blank)}>
          New brief
        </Button>
      </header>
      <nav aria-label="Workflow briefs" className="flex flex-wrap gap-2">
        {briefs.map((b) => (
          <Button
            key={b.id}
            variant={b.id === draft.id ? "secondary" : "outline"}
            disabled={busy}
            onClick={() => void select(b)}
          >
            {b.title} · {b.stage}
          </Button>
        ))}
      </nav>
      {!briefs.length && (
        <Empty>
          <EmptyHeader>
            <EmptyDescription>Start with an outcome, then describe each actor's part.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}
      {stale && (
        <div className="flex items-center gap-2">
          <p role="status" className="text-warning text-sm">
            This brief changed elsewhere. Reload before saving.
          </p>
          <Button variant="outline" onClick={() => latest && void select(latest)}>
            Reload
          </Button>
        </div>
      )}
      <div className="flex items-center gap-2">
        <Badge variant="outline">{dirty ? "Unsaved draft" : draft.stage}</Badge>
        {draft.id && <span className="text-muted-foreground text-xs">Revision {draft.revision}</span>}
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor={`${win.id}-title`}>Title</Label>
        <Input
          id={`${win.id}-title`}
          value={draft.title}
          disabled={busy}
          onChange={(e) => edit("title", e.target.value)}
        />
      </div>
      {fields.map(([key, label, hint]) => (
        <div key={key} className="flex flex-col gap-2">
          <Label htmlFor={`${win.id}-${key}`}>{label}</Label>
          <Textarea
            id={`${win.id}-${key}`}
            rows={3}
            placeholder={hint}
            value={draft[key]}
            disabled={busy}
            onChange={(e) => edit(key, e.target.value)}
          />
        </div>
      ))}
      <footer className="flex flex-wrap gap-2">
        <Button
          disabled={busy || stale || !draft.title.trim() || !draft.outcome.trim()}
          onClick={() => void save("draft")}
        >
          Save draft
        </Button>
        <Button
          variant="outline"
          disabled={busy || stale || dirty || !draft.id || draft.stage !== "draft"}
          onClick={() => void save("review")}
        >
          Request review
        </Button>
        <Button
          variant="outline"
          disabled={busy || stale || dirty || draft.stage !== "review"}
          onClick={() => void save("ready")}
        >
          Mark ready
        </Button>
        <Button
          variant="outline"
          disabled={busy || stale || dirty || !draft.id || draft.stage === "draft"}
          onClick={() => void prepare()}
        >
          Prepare design handoff
        </Button>
      </footer>
      {reviewText && !stale && (
        <section className="flex flex-col gap-2">
          <p className="text-muted-foreground text-sm">
            Prepared for review. Nothing has been sent. Copy this brief into a Design request when ready.
          </p>
          <Textarea aria-label="Design handoff" rows={12} readOnly value={reviewText} />
          <Button
            variant="outline"
            onClick={() =>
              void navigator.clipboard
                .writeText(reviewText)
                .then(() => setCopied(true))
                .catch((e: Error) => setError(e.message))
            }
          >
            {copied ? "Copied" : "Copy handoff"}
          </Button>
        </section>
      )}
    </div>
  )
}
export default { views: { "workflows/main": Workflows } } satisfies ModuleWeb
