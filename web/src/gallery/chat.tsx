/** The chat components with a canned conversation: no agent, no server. */
import { Bot } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import {
  Conversation,
  ConversationContent,
  ConversationEmptyState,
  ConversationScrollButton,
  ConversationViewport,
} from "@/components/ui/conversation"
import { Markdown, MarkdownExtensions, type MarkdownExtensionsValue } from "@/components/ui/markdown"
import { Message, MessageContent, MessageNotice } from "@/components/ui/message"
import { Tool, ToolContent, ToolHeader, ToolInput, ToolOutput } from "@/components/ui/tool"
import { ComposerSpecimens } from "./composer"
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

/** A page of text: what Notes, Drafts and Reader show. */
const DOCUMENT = `# Release notes

A note with every kind of block. Jump to [the code](#code) or [the table](#a-table).

## Checklist

- [x] Parse GitHub-flavoured Markdown
- [x] ~~Plain text~~ rich text everywhere
- [ ] Ship it, see https://example.invalid/ship

## Code

\`\`\`go
// Hello says hello.
func Hello(name string) string {
\treturn fmt.Sprintf("hello, %s", name) // 42
}
\`\`\`

\`\`\`ts
const n: number = await count("notes")
\`\`\`

\`\`\`bash
make demo && ./bin/demo serve --port 8199
\`\`\`

## A table

| Module | Shows | Size |
|:---|:---:|---:|
| Notes | the note | document |
| Chat | an answer | default |

> A quote, with \`inline code\` and **bold**.

---

1. First
2. Second
   - nested`

/** References as chips: what a page adds with MarkdownExtensions, without changing Markdown. */
const CITED =
  "The plan is in the brief [1](#ref-1) and the review [2](#ref-2); see [the docs](https://example.invalid/)."
const citeChips: MarkdownExtensionsValue = {
  components: {
    a: ({ node: _n, href, children }) =>
      href?.startsWith("#ref-") ? (
        <Badge variant="secondary">{children}</Badge>
      ) : (
        <a className="text-primary underline" href={href} target="_blank" rel="noreferrer noopener">
          {children}
        </a>
      ),
  },
}
const quoteAsBadge: MarkdownExtensionsValue["components"] = {
  blockquote: ({ node: _n, children }) => <Badge variant="outline">{children}</Badge>,
}

export function Chat() {
  return (
    <>
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
      </Section>
      <Section
        title="Markdown"
        about="The one renderer for text. GFM (tables, task lists, strikethrough, autolinks), coloured code with copy, heading ids; size document for a page. Extend with remarkPlugins, rehypePlugins and components (props, appended or merged over the defaults) or once for a page with the MarkdownExtensions context."
      >
        <Specimen label='Markdown size="document"' wide>
          <div className="w-full">
            <Markdown size="document">{DOCUMENT}</Markdown>
          </div>
        </Specimen>
        <Specimen label="Markdown (default size), the same text">
          <div className="w-full">
            <Markdown>{DOCUMENT}</Markdown>
          </div>
        </Specimen>
        <Specimen label="MarkdownExtensions: components.a draws #ref- links as chips">
          <div className="flex w-full flex-col gap-2">
            <MarkdownExtensions value={citeChips}>
              <Markdown>{CITED}</Markdown>
            </MarkdownExtensions>
            <Markdown components={quoteAsBadge}>{"> components prop: this quote is a Badge"}</Markdown>
          </div>
        </Specimen>
      </Section>
      <Section
        title="Chat composer"
        about="The input where the owner types to the agent: grows with its text, send, attachments and shortcuts, in each state (design job d_03adcb6b)."
        draft="d_03adcb6b"
      >
        <ComposerSpecimens />
      </Section>
    </>
  )
}
