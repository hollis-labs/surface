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
import { Approval } from "./approval"
import { Section, Specimen } from "./Specimen"

/** The states a card can be drawn in, and "stale": open, but changed under the person. */
const STATES: { label: string; state: CardState; stale?: boolean }[] = [
  { label: "open", state: "open" },
  { label: "pending", state: "pending" },
  { label: "submitted", state: "submitted" },
  { label: "canceled", state: "canceled" },
  { label: "expired", state: "expired" },
  { label: "superseded", state: "superseded" },
  { label: "failed", state: "failed" },
  { label: "stale", state: "open", stale: true },
]

const base = (s: (typeof STATES)[number]): AnswerableCardProps => ({
  state: s.state,
  stale: s.stale,
  onReload: s.stale ? () => {} : undefined,
  busy: s.state === "pending",
  receipt: s.state === "submitted" ? "Approved" : undefined,
  expires: s.state === "open" ? "at 14:05" : undefined,
  onRespond: () => {},
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
