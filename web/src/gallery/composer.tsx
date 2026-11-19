/** Composer review fixtures. Attachments and shortcuts are local examples, never uploads or agent calls. */
import { Paperclip, Slash, X } from "lucide-react"
import { useId, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { InputGroupAddon, InputGroupButton } from "@/components/ui/input-group"
import { PromptInput, PromptInputFooter, PromptInputSubmit, PromptInputTextarea } from "@/components/ui/prompt-input"
import { Specimen } from "./Specimen"

type State = "empty" | "typing" | "sending" | "disabled" | "streaming"

function ComposerFixture({ initial }: { initial: State }) {
  const id = useId()
  const field = useRef<HTMLTextAreaElement>(null)
  const [state, setState] = useState(initial)
  const [draft, setDraft] = useState(
    initial === "typing" || initial === "sending"
      ? "Review the composer in light and dark.\nKeep the attachment and shortcut actions within reach."
      : "",
  )
  const [attached, setAttached] = useState(initial === "typing")
  const [notice, setNotice] = useState("")
  const locked = state === "disabled" || state === "sending" || state === "streaming"
  const hint =
    state === "disabled"
      ? "Connect to an agent to send a message."
      : state === "sending"
        ? "Sending message… Your draft is kept until it is delivered."
        : state === "streaming"
          ? "The agent is answering. Stop to write another message."
          : "Enter sends · Shift+Enter adds a line"
  return (
    <div className="flex w-full min-w-0 flex-col gap-2" data-composer-state={initial}>
      <PromptInput
        aria-label={`${initial} composer`}
        onSubmit={(event) => {
          event.preventDefault()
          if (locked || (!draft.trim() && !attached)) return
          setState("sending")
          setNotice("")
        }}
      >
        {attached && (
          <InputGroupAddon align="block-start" aria-label="Attachments">
            <InputGroupButton
              disabled={locked}
              aria-label="Remove example attachment notes.md"
              onClick={() => setAttached(false)}
            >
              <Paperclip /> notes.md <X />
            </InputGroupButton>
          </InputGroupAddon>
        )}
        <PromptInputTextarea
          ref={field}
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          disabled={state === "disabled"}
          readOnly={state === "sending" || state === "streaming"}
          placeholder="Message the agent…"
          aria-label={`${initial} message`}
          aria-describedby={`${id}-hint`}
        />
        <PromptInputFooter>
          <div className="flex items-center gap-1">
            <InputGroupButton
              size="icon-xs"
              disabled={locked}
              aria-label="Add example attachment"
              title="Add example attachment"
              onClick={() => {
                setAttached(true)
                setNotice("Example attachment added. No file was uploaded.")
              }}
            >
              <Paperclip />
            </InputGroupButton>
            <InputGroupButton
              size="icon-xs"
              disabled={locked}
              aria-label="Insert example shortcut"
              title="Insert /summarize"
              onClick={() => {
                setDraft((text) => `${text}${text ? "\n" : ""}/summarize `)
                field.current?.focus()
              }}
            >
              <Slash />
            </InputGroupButton>
          </div>
          <PromptInputSubmit
            status={state === "sending" ? "sending" : state === "streaming" ? "streaming" : "ready"}
            disabled={state === "disabled" || (state !== "streaming" && !draft.trim() && !attached)}
            onStop={() => {
              setState("typing")
              setNotice("Stopped. You can continue writing.")
              field.current?.focus()
            }}
          />
        </PromptInputFooter>
      </PromptInput>
      <p id={`${id}-hint`} className="text-xs text-muted-foreground">
        {hint}
      </p>
      <p role="status" className="text-xs text-muted-foreground">
        {notice || (state === "sending" ? "Demo: waiting for delivery." : "")}
      </p>
      {state === "sending" && (
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            setState("empty")
            setDraft("")
            setAttached(false)
            setNotice("Demo message delivered.")
            field.current?.focus()
          }}
        >
          Simulate delivery
        </Button>
      )}
    </div>
  )
}

export function ComposerSpecimens() {
  return (
    <>
      <Specimen label="Composer · empty">
        <ComposerFixture initial="empty" />
      </Specimen>
      <Specimen label="Composer · typing with attachment">
        <ComposerFixture initial="typing" />
      </Specimen>
      <Specimen label="Composer · sending">
        <ComposerFixture initial="sending" />
      </Specimen>
      <Specimen label="Composer · disabled">
        <ComposerFixture initial="disabled" />
      </Specimen>
      <Specimen label="Composer · streaming / stop">
        <ComposerFixture initial="streaming" />
      </Specimen>
    </>
  )
}
