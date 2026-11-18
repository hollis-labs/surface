import * as React from "react"
import { cn } from "cn"

/**
 * One message, after AI Elements' Message: the person's on the right in a bubble, the
 * agent's across the width. MessageNotice is a line from the application (stopped, an error).
 */
function Message({ className, from, ...props }: React.ComponentProps<"div"> & { from: "user" | "assistant" }) {
  return (
    <div
      data-slot="message"
      data-from={from}
      className={cn("group/message flex w-full", from === "user" ? "justify-end" : "justify-start", className)}
      {...props}
    />
  )
}

function MessageContent({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="message-content"
      className={cn(
        "flex min-w-0 flex-col gap-2 text-sm text-foreground",
        "group-data-[from=assistant]/message:w-full",
        "group-data-[from=user]/message:max-w-[85%] group-data-[from=user]/message:rounded-lg group-data-[from=user]/message:bg-secondary group-data-[from=user]/message:px-3 group-data-[from=user]/message:py-2 group-data-[from=user]/message:whitespace-pre-wrap group-data-[from=user]/message:text-secondary-foreground",
        className
      )}
      {...props}
    />
  )
}

function MessageNotice({ className, tone = "quiet", ...props }: React.ComponentProps<"p"> & { tone?: "quiet" | "error" }) {
  return (
    <p
      data-slot="message-notice"
      data-tone={tone}
      className={cn("text-xs", tone === "error" ? "text-destructive" : "text-muted-foreground", className)}
      {...props}
    />
  )
}

export { Message, MessageContent, MessageNotice }
