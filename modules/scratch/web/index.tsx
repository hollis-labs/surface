/** Scratch's window: a box to jot into and the list of what was jotted. */
import { Icon, type ModuleWeb, useEvent, useSetting, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Item, ItemActions, ItemContent, ItemMedia, ItemTitle } from "@/components/ui/item"
import { Textarea } from "@/components/ui/textarea"

interface ScratchItem {
  id: string
  text: string
  kind: "note" | "todo" | "idea" | "followup"
  done: boolean
  created: string
}

const KIND_ICON: Record<ScratchItem["kind"], string> = {
  note: "sticky-note",
  todo: "square-check",
  idea: "lightbulb",
  followup: "corner-up-right",
}

function Scratch({ win, desk, api }: ViewProps) {
  const [items, setItems] = useState<ScratchItem[]>([])
  const [text, setText] = useState("")
  const [filter, setFilter] = useState("")
  const [error, setError] = useState("")
  const showDone = useSetting<boolean>("scratch.show_done")
  const box = useRef<HTMLTextAreaElement>(null)

  const load = useCallback(() => {
    api
      .get<ScratchItem[]>(`items${filter ? `?q=${encodeURIComponent(filter)}` : ""}`)
      .then(setItems)
      .catch((e: Error) => setError(e.message))
  }, [api, filter])
  useEffect(load, [load])
  // The agent, a command or the CLI may have changed the items: the module says so on the bus.
  useEvent("scratch.changed", load)

  // biome-ignore lint/correctness/useExhaustiveDependencies: opening with compose again refocuses the box
  useEffect(() => {
    if (win.focused) box.current?.focus()
  }, [win.focused, win.args.compose])

  // What this window is showing, for the agent and the context slots.
  useEffect(() => {
    desk.report(win.id, { kind: "data", ref: "scratch:items", filter: filter || undefined })
  }, [desk, win.id, filter])

  const add = () => {
    const t = text.trim()
    if (!t) return
    setError("")
    api
      .post("items", { text: t })
      .then(() => setText(""))
      .catch((e: Error) => setError(e.message))
  }

  const open = items.filter((i) => !i.done).length
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex flex-col gap-2 border-b p-3">
        <Textarea
          ref={box}
          rows={2}
          value={text}
          aria-label="Jot something down"
          placeholder="Jot something down. Start with “todo:”, “idea:” or “followup:” to file it."
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault()
              add()
            }
          }}
        />
        <div className="flex items-center gap-2">
          <Input
            className="flex-1"
            placeholder="Filter…"
            aria-label="Filter"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
          <Button disabled={!text.trim()} onClick={add}>
            Keep
          </Button>
        </div>
        {error && <p className="text-destructive">{error}</p>}
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-1.5">
        {items.length === 0 && (
          <Empty>
            <EmptyHeader>
              <EmptyDescription>Nothing here{filter ? " matches" : " yet"}.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        {items.map((it) => (
          <Item key={it.id} size="sm">
            <ItemMedia>
              <Button
                variant="ghost"
                size="icon-xs"
                title={it.done ? "Open again" : "Done"}
                aria-pressed={it.done}
                onClick={() => void api.put(`items/${it.id}`, { done: !it.done })}
              >
                <Icon name={it.done ? "circle-check" : "circle"} />
              </Button>
            </ItemMedia>
            <ItemContent>
              <ItemTitle>
                <Icon name={KIND_ICON[it.kind]} size={13} className="shrink-0 text-muted-foreground" />
                <span
                  className={`whitespace-pre-wrap break-words ${it.done ? "text-muted-foreground line-through" : ""}`}
                >
                  {it.text}
                </span>
              </ItemTitle>
            </ItemContent>
            <ItemActions>
              <Button variant="ghost" size="icon-xs" title="Put away" onClick={() => void api.del(`items/${it.id}`)}>
                <Icon name="x" />
              </Button>
            </ItemActions>
          </Item>
        ))}
      </div>
      <footer className="border-t px-3 py-1.5 text-muted-foreground text-xs">
        {open} open{showDone ? ` · ${items.length - open} done` : " · finished items hidden"}
      </footer>
    </div>
  )
}

export default { views: { "scratch/main": Scratch } } satisfies ModuleWeb
