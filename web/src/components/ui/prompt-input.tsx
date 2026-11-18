import * as React from "react"
import { ArrowUpIcon, SquareIcon } from "lucide-react"
import { cn } from "cn"
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupTextarea } from "@/components/ui/input-group"

/**
 * Where a message is written, after AI Elements' PromptInput: a form around an InputGroup,
 * a textarea that grows with its text (Enter sends, Shift+Enter breaks the line), and a
 * footer with the send button, which is a stop button while the agent answers.
 */
function PromptInput({ className, children, ...props }: React.ComponentProps<"form">) {
  return (
    <form data-slot="prompt-input" className={cn("w-full", className)} {...props}>
      <InputGroup className="h-auto flex-col">{children}</InputGroup>
    </form>
  )
}

function PromptInputTextarea({ className, onKeyDown, ...props }: React.ComponentProps<typeof InputGroupTextarea>) {
  return (
    <InputGroupTextarea
      data-slot="prompt-input-textarea"
      className={cn("field-sizing-content max-h-48 min-h-14 px-3", className)}
      onKeyDown={(e) => {
        onKeyDown?.(e)
        if (e.defaultPrevented) return
        if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
          e.preventDefault()
          e.currentTarget.form?.requestSubmit()
        }
      }}
      {...props}
    />
  )
}

function PromptInputFooter({ className, ...props }: React.ComponentProps<typeof InputGroupAddon>) {
  return <InputGroupAddon align="block-end" className={cn("justify-between gap-2", className)} {...props} />
}

function PromptInputSubmit({
  status = "ready",
  onStop,
  className,
  ...props
}: Omit<React.ComponentProps<typeof InputGroupButton>, "type" | "children"> & {
  status?: "ready" | "streaming"
  onStop?: () => void
}) {
  const streaming = status === "streaming"
  return (
    <InputGroupButton
      data-slot="prompt-input-submit"
      variant="default"
      size="icon-xs"
      className={cn("ml-auto rounded-full", className)}
      aria-label={streaming ? "Stop" : "Send"}
      {...props}
      type={streaming ? "button" : "submit"}
      onClick={streaming ? onStop : props.onClick}
    >
      {streaming ? <SquareIcon className="fill-current" /> : <ArrowUpIcon />}
    </InputGroupButton>
  )
}

export { PromptInput, PromptInputTextarea, PromptInputFooter, PromptInputSubmit }
