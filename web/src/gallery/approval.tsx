/** card-approval, as proposed in design job d_d065599e: risk, before/after, per-change, states, in context. */
import { CardApproval, type CardApprovalChange } from "@/components/ui/card-approval"
import { CardConfirm } from "@/components/ui/card-confirm"
import type { AnswerableCardProps } from "@/components/ui/card-frame"
import { CardNotice } from "@/components/ui/card-notice"
import { CardPlan } from "@/components/ui/card-plan"
import { CardProgress } from "@/components/ui/card-progress"
import { Section, Specimen } from "./Specimen"

const open: AnswerableCardProps = { state: "open", expires: "in 28 min", onRespond: () => {} }

const SHORT: CardApprovalChange[] = [
  { label: "Status", before: "ready for review", after: "approved" },
  { label: "Commit", after: "9f517de" },
  { label: "Branch", after: "design/d065599e-approval-card-mockup" },
]

const LONG: CardApprovalChange[] = [
  { label: "Title", before: "Launch", after: "Launch notes" },
  {
    label: "Text",
    before: "We launch on Monday.\nThe team meets at nine.",
    after:
      "We launch on Monday, 24 November.\nThe team meets at nine in the main room; the agenda is in the draft.\nAfter the launch: a short review on Wednesday, notes in the shared folder.\nQuestions go to the launch thread, not to email.",
  },
]

const SEVERAL: CardApprovalChange[] = [
  { label: "Rename the draft", before: "Untitled", after: "Launch notes" },
  { label: "Move it", before: "Drafts", after: "Projects / Launch" },
  { label: "Share it", after: "with the design team" },
]

function Wide({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Specimen label={label}>
      <div className="w-full">{children}</div>
    </Specimen>
  )
}

export function Approval() {
  return (
    <>
      <Section
        title="Approval card: risk at a glance"
        about="Proposal for job d_d065599e. Risk shows in the left edge, the icon and the badge. High risk is the only red, and its Approve leads to a second confirm."
      >
        {(["low", "medium", "high"] as const).map((risk) => (
          <Wide key={risk} label={`risk="${risk}"`}>
            <CardApproval
              {...open}
              title={{ low: "Rename a note", medium: "Approve the design", high: "Delete 14 notes" }[risk]}
              summary={
                {
                  low: "The agent wants to rename one note.",
                  medium: "Approve PR 18 at commit 9f517de. The Design team is told to merge exactly that commit.",
                  high: "The agent wants to delete 14 notes from **Archive**. They cannot be restored from Surface.",
                }[risk]
              }
              risk={risk}
              requestedBy={risk === "medium" ? "design" : "agent"}
              changes={
                risk === "low"
                  ? [{ label: "Title", before: "untitled", after: "Weekly review" }]
                  : risk === "medium"
                    ? SHORT
                    : [{ label: "Notes", before: "14", after: "0" }]
              }
            />
          </Wide>
        ))}
        <Wide label="high, after Approve…: the second confirm (with a typed phrase)">
          <ConfirmStep />
        </Wide>
      </Section>
      <Section
        title="Approval card: before and after"
        about="Short values sit on one line. Long ones stack as before and after blocks, clipped until opened."
      >
        <Wide label="short values">
          <CardApproval
            {...open}
            title="Write the draft"
            summary="Two fields change."
            risk="low"
            changes={[
              { label: "Title", before: "Launch", after: "Launch notes" },
              { label: "Lines", before: "4", after: "7" },
            ]}
          />
        </Wide>
        <Wide label="a longer text">
          <CardApproval
            {...open}
            title="Rewrite the draft"
            summary="The agent wants to replace the draft's text."
            risk="medium"
            changes={LONG}
            requestedBy="agent"
          />
        </Wide>
      </Section>
      <Section
        title="Approval card: several changes"
        about="With perChange, each change is approved or declined on its own. The button says how many are approved, and each change is answered as its own decision."
      >
        <Wide label="perChange, all approved">
          <CardApproval
            {...open}
            title="Tidy the launch draft"
            summary="Three changes; take any of them."
            risk="medium"
            perChange
            changes={SEVERAL}
          />
        </Wide>
        <Wide label="perChange, answered: 2 of 3">
          <CardApproval
            state="submitted"
            onRespond={() => {}}
            title="Tidy the launch draft"
            risk="medium"
            perChange
            changes={SEVERAL}
            decision={{ approved: 2, of: 3 }}
          />
        </Wide>
      </Section>
      <Section
        title="Approval card: states"
        about="Waiting shows when it expires. Once closed, the edge and colour go quiet and the foot says what happened."
      >
        <Wide label="waiting for an answer">
          <CardApproval
            {...open}
            title="Approve the design"
            summary="PR 18 at 9f517de."
            risk="medium"
            changes={SHORT.slice(0, 2)}
          />
        </Wide>
        <Wide label="working (answer being sent)">
          <CardApproval
            state="pending"
            busy
            onRespond={() => {}}
            title="Approve the design"
            summary="PR 18 at 9f517de."
            risk="medium"
            changes={SHORT.slice(0, 2)}
          />
        </Wide>
        <Wide label="approved">
          <CardApproval
            state="submitted"
            decision="approved"
            onRespond={() => {}}
            title="Approve the design"
            summary="PR 18 at 9f517de."
            risk="medium"
            changes={SHORT.slice(0, 2)}
          />
        </Wide>
        <Wide label="declined">
          <CardApproval
            state="submitted"
            decision="declined"
            onRespond={() => {}}
            title="Delete 14 notes"
            summary="From Archive."
            risk="high"
          />
        </Wide>
        <Wide label="expired">
          <CardApproval
            state="expired"
            onRespond={() => {}}
            title="Approve the design"
            summary="PR 18 at 7f7d61f."
            risk="medium"
          />
        </Wide>
        <Wide label="replaced by a newer card">
          <CardApproval
            state="superseded"
            onRespond={() => {}}
            title="Approve the design at 7f7d61f"
            summary="A newer commit came in; you are asked again on it."
            risk="medium"
          />
        </Wide>
      </Section>
      <Section
        title="Approval card: next to the other cards"
        about="In a column of cards, the approval is the one with an edge and a decision. Notices, progress and plans stay flat."
      >
        <Specimen label="a column of cards" wide>
          <div className="mx-auto flex w-full max-w-md flex-col gap-2">
            <CardNotice
              title="Ready to review"
              body="“Approval card mockup” has a preview to review: its window is open."
              tone="info"
              onDismiss={() => {}}
            />
            <CardProgress title="Reading the branch" label="files" value={3} max={5} status="running" />
            <CardPlan
              title="Design round 1"
              steps={[
                { id: "1", title: "Brief sent", status: "done" },
                { id: "2", title: "Mock-up pushed", status: "done" },
                { id: "3", title: "Your review", status: "active" },
              ]}
            />
            <CardApproval
              {...open}
              title="Approve the design at 9f517de"
              summary="PR 18 at commit 9f517de."
              risk="medium"
              changes={SHORT.slice(0, 2)}
              requestedBy="design"
            />
            <CardConfirm {...open} title="Close three windows?" body="Your unsaved draft stays open." danger={false} />
          </div>
        </Specimen>
      </Section>
    </>
  )
}

/** The high-risk confirm step, opened: the gallery clicks Approve… once on mount. */
function ConfirmStep() {
  return (
    <div
      ref={(el) => {
        if (!el) return
        const b = Array.from(el.querySelectorAll<HTMLButtonElement>("button")).find((x) => x.textContent?.endsWith("…"))
        if (b && !el.dataset.clicked) {
          el.dataset.clicked = "1"
          setTimeout(() => b.click(), 0)
        }
      }}
    >
      <CardApproval
        {...open}
        title="Delete 14 notes"
        summary="The agent wants to delete 14 notes from **Archive**."
        risk="high"
        phrase="delete 14 notes"
        requestedBy="agent"
        changes={[{ label: "Notes", before: "14", after: "0" }]}
      />
    </div>
  )
}
