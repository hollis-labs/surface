import type * as React from "react"

import { cn } from "cn"

/**
 * A plain writing surface: a textarea with no frame, in the monospace face, that fills
 * the room it is given. For editing a document's source (a note, a draft), where the
 * bordered Textarea of a form would be wrong. Surface's own; not from shadcn.
 */
function TextEditor({ className, ...props }: React.ComponentProps<"textarea">) {
  return (
    <textarea
      data-slot="text-editor"
      className={cn(
        "block size-full min-h-0 resize-none border-0 bg-transparent px-4 py-3 font-mono text-sm leading-relaxed text-foreground outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50",
        className
      )}
      {...props}
    />
  )
}

export { TextEditor }
