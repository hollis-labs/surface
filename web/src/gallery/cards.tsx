/** Every card in every state, from fixtures. Answering does nothing here. */
import { CardApproval } from "@/components/ui/card-approval"
import { CardChoice } from "@/components/ui/card-choice"
import { CardConfirm } from "@/components/ui/card-confirm"
import { CardForm, type CardFormField } from "@/components/ui/card-form"
import type { AnswerableCardProps, CardState } from "@/components/ui/card-frame"
import { CardMiss } from "@/components/ui/card-miss"
import { CardNotice } from "@/components/ui/card-notice"
import { CardPlan } from "@/components/ui/card-plan"
import { CardProgress } from "@/components/ui/card-progress"
import { CardPrompt } from "@/components/ui/card-prompt"
import { ModuleEntityCardBody } from "../cards/EntityCard"
import { AlignProposalCardRenderer } from "../desk/AlignWorkbench"
import { Approval } from "./approval"
import { Section, Specimen } from "./Specimen"

/** The states a card can be drawn in, and "stale": open, but changed under the person. */
const STATES: { label: string; state: CardState; stale?: boolean; draft?: boolean }[] = [
  { label: "open", state: "open" },
  { label: "pending", state: "pending" },
  { label: "submitted", state: "submitted" },
  { label: "canceled", state: "canceled" },
  { label: "expired", state: "expired" },
  { label: "abandoned", state: "abandoned" },
  { label: "superseded", state: "superseded" },
  { label: "failed", state: "failed" },
  { label: "stale", state: "open", stale: true },
  { label: "draft restored", state: "open", draft: true },
]

/** What the owner had typed before a reload, as the page keeps it: each card reads its own
 * names (a form its field keys, a prompt `text`, a choice `picked` and `other`). */
const DRAFT = { title: "Research, from a draft", count: 3, text: "Kept across a reload", picked: ["below"], other: "" }

const base = (s: (typeof STATES)[number]): AnswerableCardProps => ({
  state: s.state,
  stale: s.stale,
  onReload: s.stale ? () => {} : undefined,
  busy: s.state === "pending",
  receipt: s.state === "submitted" ? "Approved" : undefined,
  expires: s.state === "open" ? "at 14:05" : undefined,
  onRespond: () => {},
  draft: s.draft ? DRAFT : undefined,
  onDraft: s.draft ? () => {} : undefined,
})

const FIELDS: CardFormField[] = [
  { key: "title", kind: "text", title: "Title", required: true },
  { key: "about", kind: "textarea", title: "About", description: "A line or two." },
  { key: "count", kind: "integer", title: "How many" },
  { key: "public", kind: "boolean", title: "Public" },
  { key: "kind", kind: "enum", title: "Kind", options: ["note", "draft", "plan"] },
]

/** One answerable card kind, drawn in each state. */
function AllStates({ name, draw }: { name: string; draw: (p: AnswerableCardProps) => React.ReactNode }) {
  return (
    <Section title={name}>
      {STATES.map((s) => (
        <Specimen key={s.label} label={s.label}>
          <div className="w-full">{draw(base(s))}</div>
        </Specimen>
      ))}
    </Section>
  )
}

export function Cards() {
  return (
    <>
      <Section
        title="Module Entity Cards & Visual Alignment Proposal Cards"
        about="Agents can embed live domain cards from Ops (Kanban workstreams, Agent sessions, Fleet hosts), Registry records, Native Trackers, and Visual Alignment Proposals directly in chat or the Cards drawer."
      >
        <Specimen label="Ops Kanban Workstream Card (ops.workstream)">
          <div className="w-full">
            <ModuleEntityCardBody
              entity={{
                id: "CW-20261122-0008",
                entity_kind: "ops.workstream",
                ref: "cite:ops/workstream/CW-20261122-0008",
                title: "Module Entity Cards & Live Record Resolution",
                subtitle: "CW-20261122-0008 · Surface ADE",
                status: "active",
                stage: "Implementation",
                progress: { done: 3, total: 4 },
                purpose:
                  "Allow the agent to render and ask questions with live Ops Kanban cards, Agent cards, Fleet host cards, Registry records, and Tracker items.",
                latest_activity:
                  "EntityCardRenderer wired for card_show & card_ask with live /api/cite/resolve fallback.",
                tags: ["surface-next", "cards", "ops"],
              }}
            />
          </div>
        </Specimen>
        <Specimen label="Ops Live Agent Session Card (ops.agent)">
          <div className="w-full">
            <ModuleEntityCardBody
              entity={{
                id: "agt-surface-fe",
                entity_kind: "ops.agent",
                ref: "cite:ops/agent/agt-surface-fe",
                title: "surface-frontend-architect",
                status: "active",
                model: "claude-opus-4-6",
                host: "surface-desk.local",
                owner: "seat-01 (Primary ADE)",
                stage: "local · worktree/main",
                telemetry: { tokens_pct: 42 },
                summary: "Implementing Slice 14 visual alignment workbench, drop pad, and native trackers.",
              }}
            />
          </div>
        </Specimen>
        <Specimen label="Fleet Server Host Card (ops.host)">
          <div className="w-full">
            <ModuleEntityCardBody
              entity={{
                id: "example-host",
                entity_kind: "ops.host",
                ref: "cite:ops/host/example-host",
                title: "example-host.example.test",
                subtitle: "192.0.2.10:8190 · Linux 6.12 NixOS",
                status: "online",
                telemetry: {
                  uptime: "18d 07h",
                  cpu_pct: 24,
                  mem_pct: 58,
                  disk_pct: 41,
                },
              }}
            />
          </div>
        </Specimen>
        <Specimen label="Native Tracker Item Card (tracker.item)">
          <div className="w-full">
            <ModuleEntityCardBody
              entity={{
                id: "trk-item-01",
                entity_kind: "tracker.item",
                title: "Interactive Architecture Flow & Bucket Triage Approval Loop",
                stage: "Visual Alignment",
                status: "in_progress",
                summary: "Co-editable architecture lanes, drag-and-drop triage buckets, and phased timeline itinerary.",
                tags: ["alignment", "drop-pad", "ux"],
              }}
            />
          </div>
        </Specimen>
        <Specimen label="Visual Alignment Proposal Card in Chat (kind='align')" wide>
          <div className="w-full">
            <AlignProposalCardRenderer
              card={{
                id: "card-align-demo-1",
                kind: "align",
                version: "1",
                title: "Proposal: Surface Visual Alignment & Drop Pad Architecture",
                status: "pending",
                payload: {
                  proposal_id: "prop-arch-01",
                  mode: "architecture",
                  question:
                    "Review or adjust the 3-lane architecture flow in the Alignment Workbench, then approve to save as a citeable reference.",
                  summary:
                    "Adds Visual Alignment Proposals, Drop Pad file/ref collections, and Native Trackers with arbitrary agent metadata/extras JSON.",
                  nodes: [
                    { id: "n1", label: "Entity Card Renderer", lane: "Frontend", delta: "added" },
                    { id: "n2", label: "Align & Drop Pad Workbench", lane: "Frontend", delta: "added" },
                    { id: "n3", label: "Cite & Card Store", lane: "Backend", delta: "modified" },
                    { id: "n4", label: "Trackers SQLite Store", lane: "Data & Storage", delta: "added" },
                  ],
                },
              }}
              respond={async () => {}}
            />
          </div>
        </Specimen>
      </Section>
      <Approval />
      <AllStates
        name="card-confirm"
        draw={(p) => (
          <CardConfirm {...p} title="Close three windows?" body="Your unsaved draft stays open." danger={false} />
        )}
      />
      <AllStates
        name="card-prompt"
        draw={(p) => (
          <CardPrompt {...p} title="A name" question="What should the flow be called?" placeholder="Research" />
        )}
      />
      <AllStates
        name="card-choice"
        draw={(p) => (
          <CardChoice
            {...p}
            title="Which layout?"
            question="Pick where the browser goes."
            options={[
              { id: "left", label: "Left", description: "Beside the draft" },
              { id: "below", label: "Below" },
              { id: "float", label: "Floating" },
            ]}
            allowOther
          />
        )}
      />
      <AllStates
        name="card-form"
        draw={(p) => (
          <CardForm
            {...p}
            title="New flow"
            body="Fill in what you know."
            fields={FIELDS}
            values={{ title: "Research" }}
          />
        )}
      />
      <AllStates
        name="card-approval"
        draw={(p) => (
          <CardApproval
            {...p}
            title="Write the draft"
            summary="The agent wants to replace the draft's text."
            risk="high"
            requestedBy="agent"
            changes={[
              { label: "Title", before: "Launch", after: "Launch notes" },
              { label: "Lines", before: "4", after: "7" },
            ]}
          />
        )}
      />
      <Section title="card-notice">
        {(["info", "success", "warning", "error"] as const).map((t) => (
          <Specimen key={t} label={`tone="${t}"`}>
            <div className="w-full">
              <CardNotice
                title={{ info: "For your information", success: "Done", warning: "Heads up", error: "It failed" }[t]}
                body="Something happened that you should know about."
                tone={t}
                detail={t === "error" ? "exit status 1" : undefined}
                onDismiss={() => {}}
              />
            </div>
          </Specimen>
        ))}
        <Specimen label="dismissed">
          <div className="w-full">
            <CardNotice title="A notice" body="Already dismissed." state="canceled" />
          </div>
        </Specimen>
      </Section>
      <Section title="card-progress and card-plan">
        {(["running", "done", "failed"] as const).map((s) => (
          <Specimen key={s} label={`progress ${s}`}>
            <div className="w-full">
              <CardProgress
                title="Reading the site"
                label="pages"
                value={s === "running" ? 4 : 10}
                max={10}
                status={s}
                detail={s === "failed" ? "The site stopped answering." : undefined}
              />
            </div>
          </Specimen>
        ))}
        <Specimen label="progress, no value">
          <div className="w-full">
            <CardProgress title="Thinking" status="running" />
          </div>
        </Specimen>
        <Specimen label="plan" wide>
          <div className="w-full">
            <CardPlan
              title="Write the article"
              steps={[
                { id: "1", title: "Read the sources", status: "done" },
                { id: "2", title: "Outline", status: "done" },
                { id: "3", title: "Draft each section", status: "active", detail: "Section 2 of 4" },
                { id: "4", title: "Images", status: "skipped" },
                { id: "5", title: "Proofread", status: "pending" },
              ]}
            />
          </div>
        </Specimen>
      </Section>
      <Section title="card-miss" about="What draws when no renderer can.">
        {(["unknown-kind", "version-skew", "failed"] as const).map((r) => (
          <Specimen key={r} label={r}>
            <div className="w-full">
              <CardMiss
                kind="survey"
                version="2"
                reason={r}
                title="A survey"
                payload={{ questions: 3 }}
                detail={r === "failed" ? "TypeError: x is undefined" : undefined}
              />
            </div>
          </Specimen>
        ))}
      </Section>
    </>
  )
}
