/** The chat components with a canned conversation: no agent, no server. */
import { Bot } from "lucide-react"
import { useState } from "react"
import {
  Conversation,
  ConversationContent,
  ConversationEmptyState,
  ConversationScrollButton,
  ConversationViewport,
} from "@/components/ui/conversation"
import { Markdown } from "@/components/ui/markdown"
import { Message, MessageContent, MessageNotice } from "@/components/ui/message"
import { PromptInput, PromptInputFooter, PromptInputSubmit, PromptInputTextarea } from "@/components/ui/prompt-input"
import { Tool, ToolContent, ToolHeader, ToolInput, ToolOutput } from "@/components/ui/tool"
import { Section, Specimen } from "./Specimen"

const ANSWER = `Here is the layout I set up:

- **Browser** on the left, at the page you named
- **Drafts** on the right, in front
- *Scratch* below it, for loose ends

\`\`\`json
{ "mode": "research", "ratio": 0.6 }
\`\`\`

| Window | Where |
|---|---|
| Browser | left |
| Drafts | right |`

/** Half an answer, as it is while streaming: the open bold and code are closed for drawing. */
const STREAMING = "Opening the **Drafts window and `drafts_get"

export function Chat() {
  const [draft, setDraft] = useState("")
  return (
    <Section title="Conversation" about="A canned thread: the person, the agent with Markdown, tool rows, a notice.">
      <Specimen label="Conversation, Message, Markdown, Tool, MessageNotice" wide>
        <div className="h-[52rem] w-full overflow-hidden rounded-md border border-border">
          <Conversation className="h-full">
            <ConversationViewport>
              <ConversationContent>
                <Message from="user">
                  <MessageContent>Set up a research layout for the article.</MessageContent>
                </Message>
                <Tool>
                  <ToolHeader title="desk_arrange" status="done" />
                  <ToolContent>
                    <ToolInput input={{ windows: [{ view: "browser" }, { view: "drafts", focus: true }] }} />
                    <ToolOutput output='{"arranged": true}' />
                  </ToolContent>
                </Tool>
                <Tool defaultOpen>
                  <ToolHeader title="browser_read" status="error" />
                  <ToolContent>
                    <ToolInput input={{ url: "https://example.invalid/" }} />
                    <ToolOutput output="The site could not be reached." error />
                  </ToolContent>
                </Tool>
                <Tool>
                  <ToolHeader title="drafts_get" status="running" />
                </Tool>
                <Message from="assistant">
                  <MessageContent>
                    <Markdown>{ANSWER}</Markdown>
                  </MessageContent>
                </Message>
                <Message from="assistant">
                  <MessageContent>
                    <Markdown streaming>{STREAMING}</Markdown>
                  </MessageContent>
                </Message>
                <MessageNotice>Stopped.</MessageNotice>
                <MessageNotice tone="error">The agent could not finish.</MessageNotice>
              </ConversationContent>
            </ConversationViewport>
            <ConversationScrollButton />
          </Conversation>
        </div>
      </Specimen>
      <Specimen label="ConversationEmptyState">
        <ConversationEmptyState
          icon={<Bot />}
          title="Ask the agent"
          description="It sees the window in front and can work the desk for you."
        />
      </Specimen>
      <Specimen label="PromptInput: ready and streaming">
        <div className="flex w-full flex-col gap-2">
          <PromptInput onSubmit={(e) => e.preventDefault()}>
            <PromptInputTextarea
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder="Ask, or /open, /layout, /run"
              aria-label="Message"
            />
            <PromptInputFooter>
              <span className="text-xs text-muted-foreground">Enter sends</span>
              <PromptInputSubmit disabled={!draft.trim()} />
            </PromptInputFooter>
          </PromptInput>
          <PromptInput onSubmit={(e) => e.preventDefault()}>
            <PromptInputTextarea placeholder="The agent is answering" aria-label="Message while streaming" />
            <PromptInputFooter>
              <span className="text-xs text-muted-foreground">working…</span>
              <PromptInputSubmit status="streaming" onStop={() => {}} />
            </PromptInputFooter>
          </PromptInput>
        </div>
      </Specimen>
    </Section>
  )
}
