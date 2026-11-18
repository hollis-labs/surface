/**
 * The Interaction Builder: describe a way of working and the agent sets the app up for it
 * (windows, mode, instructions, reflexes), saved as a flow to come back to and refine.
 *
 * The view composes nothing and takes no description itself: the owner talks to the agent in
 * its panel, whose builder slot knows how, and this window lists the flows the agent saved.
 */
import { type ModuleWeb, useEvent, type ViewProps } from "@surface/sdk"
import { Bot, ChevronDown, ChevronUp, Play, Trash2, Workflow } from "lucide-react"
import { useCallback, useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Item, ItemActions, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"

interface LayoutWindow {
  view: string
  title?: string
  focus?: boolean
}
interface Flow {
  name: string
  goal: string
  description?: string
  version: number
  updated: string
  layout?: { mode: string; ratio?: number; windows: LayoutWindow[]; setup?: Record<string, unknown> }
  versions?: { version: number; note?: string; created: string }[]
}

function when(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })
}

function BuilderView({ win, desk, api }: ViewProps) {
  const [flows, setFlows] = useState<Flow[]>([])
  const [open, setOpen] = useState<string>(typeof win.state?.flow === "string" ? win.state.flow : "")
  const [detail, setDetail] = useState<Flow | null>(null)
  const [err, setErr] = useState("")

  const load = useCallback(() => {
    api.get<Flow[]>("flows").then(setFlows, (e: Error) => setErr(e.message))
  }, [api])
  useEffect(load, [load])
  // flows: the detail is read again whenever the list is (a new version was saved).
  // biome-ignore lint/correctness/useExhaustiveDependencies: flows is a refresh signal
  useEffect(() => {
    if (!open) {
      setDetail(null)
      return
    }
    api.get<Flow>(`flows/${encodeURIComponent(open)}`).then(setDetail, () => setDetail(null))
  }, [open, api, flows])
  useEvent("builder.changed", load)
  useEvent("desk.layouts", load)

  const select = (name: string) => {
    const next = open === name ? "" : name
    setOpen(next)
    desk.report(win.id, next ? { flow: next } : {})
  }

  const run = async (path: string) => {
    setErr("")
    try {
      await api.post(path)
      load()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }
  const remove = async (f: Flow) => {
    const ok = await desk.confirm({
      title: `Delete the flow “${f.name}”?`,
      message: "Its versions and layout go too. Its reflexes stay.",
      danger: true,
      confirmLabel: "Delete",
    })
    if (!ok) return
    await api.del(`flows/${encodeURIComponent(f.name)}`)
    setOpen("")
    load()
  }

  return (
    <div className="@container flex h-full min-h-0 flex-col overflow-y-auto">
      <section className="flex shrink-0 flex-col items-start gap-2 border-b border-border p-4 @max-md:p-3">
        <h2 className="text-sm font-semibold">Flows</h2>
        <p className="text-sm text-muted-foreground">
          Describe a way of working to the agent, in its panel: “set up a flow to triage my inbox”, “write an article”.
          It arranges the windows, sets the mode and its own instructions, adds the reflexes the flow needs, and saves
          it here. To change one, tell the agent what to change; each change is a new version.
        </p>
        <Button variant="outline" size="sm" onClick={() => void desk.run("agent.toggle", { open: true })}>
          <Bot /> Open the agent
        </Button>
        {err && <p className="text-sm text-destructive">{err}</p>}
      </section>
      <section className="flex-1">
        {flows.length === 0 && (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Workflow />
              </EmptyMedia>
              <EmptyTitle>No flows yet</EmptyTitle>
              <EmptyDescription>Ask the agent to set one up.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        <ul className="flex flex-col gap-1 p-2">
          {flows.map((f) => {
            const isOpen = open === f.name
            return (
              <li key={f.name}>
                <Item size="sm" variant={isOpen ? "muted" : "default"}>
                  <ItemContent className="min-w-0 cursor-pointer" onClick={() => select(f.name)}>
                    <ItemTitle className={isOpen ? "text-primary" : undefined}>{f.name}</ItemTitle>
                    <ItemDescription className="line-clamp-1 text-xs">
                      {f.goal} · v{f.version} · {when(f.updated)}
                    </ItemDescription>
                  </ItemContent>
                  <ItemActions>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={isOpen ? "Hide the details" : "Show the details"}
                      aria-expanded={isOpen}
                      onClick={() => select(f.name)}
                    >
                      {isOpen ? <ChevronUp /> : <ChevronDown />}
                    </Button>
                    <Tooltip>
                      <TooltipTrigger
                        render={
                          <Button
                            variant="outline"
                            size="sm"
                            aria-label="Open"
                            onClick={() => void run(`flows/${encodeURIComponent(f.name)}/open`)}
                          />
                        }
                      >
                        <Play />
                        <span className="@max-md:hidden">Open</span>
                      </TooltipTrigger>
                      <TooltipContent>Put this flow on the desk</TooltipContent>
                    </Tooltip>
                  </ItemActions>
                </Item>
                {isOpen && detail && (
                  <FlowDetail
                    flow={detail}
                    onRestore={(v) => void run(`flows/${encodeURIComponent(f.name)}/versions/${v}/restore`)}
                    onDelete={() => void remove(f)}
                  />
                )}
              </li>
            )
          })}
        </ul>
      </section>
    </div>
  )
}

function FlowDetail({
  flow,
  onRestore,
  onDelete,
}: {
  flow: Flow
  onRestore: (v: number) => void
  onDelete: () => void
}) {
  const setup = flow.layout?.setup ?? {}
  const reflexes = Array.isArray(setup.reflexes) ? (setup.reflexes as string[]) : []
  const key = "text-muted-foreground"
  return (
    <div className="flex flex-col items-start gap-2 px-3 pt-1 pb-3 text-xs">
      {flow.description && <p>{flow.description}</p>}
      <div className="flex flex-wrap items-center gap-1">
        <span className={key}>Windows ({flow.layout?.mode ?? "tile"}):</span>
        {(flow.layout?.windows ?? []).map((w, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: a layout's windows have no id; their order is their identity
          <Badge key={i} variant={w.focus ? "default" : "secondary"}>
            {w.title || w.view}
          </Badge>
        ))}
      </div>
      {typeof setup.mode === "string" && setup.mode && (
        <p>
          <span className={key}>Mode: </span>
          {setup.mode}
        </p>
      )}
      {typeof setup.instructions === "string" && setup.instructions && (
        <p>
          <span className={key}>Agent instructions: </span>
          {setup.instructions}
        </p>
      )}
      {reflexes.length > 0 && (
        <div className="flex flex-wrap items-center gap-1">
          <span className={key}>Reflexes:</span>
          {reflexes.map((r) => (
            <Badge key={r} variant="outline" className="font-mono">
              {r}
            </Badge>
          ))}
        </div>
      )}
      {flow.versions && flow.versions.length > 1 && (
        <Collapsible className="w-full">
          <CollapsibleTrigger render={<Button variant="link" size="xs" className="px-0" />}>
            Versions ({flow.versions.length})
          </CollapsibleTrigger>
          <CollapsibleContent>
            <ul className="mt-1 flex flex-col gap-1">
              {flow.versions.map((v) => (
                <li key={v.version} className="flex items-baseline gap-2">
                  <span className="w-7 shrink-0 font-semibold">v{v.version}</span>
                  <span className="min-w-0 flex-1 text-muted-foreground">
                    {v.note || "—"} · {when(v.created)}
                  </span>
                  {v.version !== flow.version && (
                    <Button variant="link" size="xs" onClick={() => onRestore(v.version)}>
                      restore
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          </CollapsibleContent>
        </Collapsible>
      )}
      <Button variant="destructive" size="xs" onClick={onDelete}>
        <Trash2 /> Delete flow
      </Button>
    </div>
  )
}

const web: ModuleWeb = { views: { "builder/main": BuilderView } }
export default web
