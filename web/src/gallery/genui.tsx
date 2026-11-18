/** Generated surfaces: each reviewed pattern, in each phase. */
import { type GenuiPattern, type GenuiPhase, GenuiSurface } from "@/components/ui/genui-surface"
import { Section, Specimen } from "./Specimen"

const PATTERNS: { name: string; pattern: GenuiPattern }[] = [
  {
    name: "approval",
    pattern: {
      kind: "approval",
      title: "Publish the post",
      summary: "The agent wants to publish the draft to the blog.",
      changes: [{ label: "Status", before: "draft", after: "published" }],
      choices: [{ label: "Publish", risk: "high" }, { label: "Keep as draft" }],
    },
  },
  {
    name: "form",
    pattern: {
      kind: "form",
      title: "New flow",
      fields: [
        { key: "name", label: "Name", type: "text", required: true, help: "What to call it." },
        { key: "about", label: "About", type: "textarea", required: false, maxLength: 200 },
        { key: "steps", label: "Steps", type: "integer", required: false, min: 1, max: 9 },
        { key: "public", label: "Public", type: "checkbox", required: false },
      ],
      draft: { name: "Research" },
      submitLabel: "Create",
    },
  },
  {
    name: "list_detail",
    pattern: {
      kind: "list_detail",
      title: "Open questions",
      items: [
        { id: "a", title: "Which theme is the baseline?", summary: "Design" },
        { id: "b", title: "Where does the registry live?", summary: "Settled" },
      ],
      selectedId: "a",
      detail: { title: "Which theme is the baseline?", body: "Decided after the plumbing, not before." },
    },
  },
]

const PHASES: { phase: GenuiPhase; status?: string; message?: string; confirmation?: string }[] = [
  { phase: "ready" },
  { phase: "busy" },
  { phase: "pending", message: "Waiting for the host." },
  { phase: "confirmation", confirmation: "This publishes the post. Go on?" },
  { phase: "unknown", message: "It may or may not have happened. Check before trying again." },
  { phase: "terminal", status: "accepted" },
  { phase: "terminal", status: "refused", message: "Not allowed here." },
  { phase: "stale" },
]

export function Genui() {
  return (
    <>
      {PATTERNS.map(({ name, pattern }) => (
        <Section key={name} title={`genui-surface: ${name}`}>
          {PHASES.map((p) => (
            <Specimen key={`${p.phase}:${p.status ?? ""}`} label={p.status ? `${p.phase} · ${p.status}` : p.phase}>
              <div className="w-full">
                <GenuiSurface
                  pattern={pattern}
                  phase={p.phase}
                  status={p.status}
                  message={p.message}
                  confirmation={p.confirmation}
                  canConfirm={p.phase === "confirmation"}
                  onChoose={() => {}}
                  onConfirm={() => {}}
                  onCheck={() => {}}
                  onSelect={() => {}}
                />
              </div>
            </Specimen>
          ))}
        </Section>
      ))}
    </>
  )
}
