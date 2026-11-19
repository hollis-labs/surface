/**
 * Work's window, for now: the Planner's workspaces, read from the module's API
 * (modules/work/README.md). The board, calendar, outline, canvas, flow and detail views come
 * in a later change, built against the same routes.
 */
import { Icon, type ModuleWeb, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Item, ItemContent, ItemDescription, ItemMedia, ItemTitle } from "@/components/ui/item"

interface Workspace {
  slug: string
  title: string
  app: string
  items: number
  archived: boolean
  updated: string
}

function Planner({ win, desk, api }: ViewProps) {
  const [spaces, setSpaces] = useState<Workspace[]>([])
  const [problem, setProblem] = useState("")

  const load = useCallback(() => {
    api
      .get<{ workspaces: Workspace[]; problem?: string }>("work?app=planner")
      .then((r) => {
        setSpaces(r.workspaces)
        setProblem(r.problem ?? "")
      })
      .catch((e: Error) => setProblem(e.message))
  }, [api])
  useEffect(load, [load])
  // The agent, another window or the CLI changed a workspace.
  useEvent("work.changed", load)

  useEffect(() => {
    desk.report(win.id, { kind: "workspace", ref: "work" })
  }, [desk, win.id])

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="min-h-0 flex-1 overflow-auto p-1.5">
        {spaces.length === 0 && (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>No Planner workspaces</EmptyTitle>
              <EmptyDescription>
                {problem || "Workspaces in Caster show here. The agent can make one with work_space_create."}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        {spaces.map((w) => (
          <Item key={w.slug} size="sm">
            <ItemMedia>
              <Icon name="map" size={14} className="text-muted-foreground" />
            </ItemMedia>
            <ItemContent>
              <ItemTitle>
                {w.title}
                {w.archived && <Badge variant="secondary">archived</Badge>}
              </ItemTitle>
              <ItemDescription>
                {w.slug} · {w.items} item{w.items === 1 ? "" : "s"}
              </ItemDescription>
            </ItemContent>
          </Item>
        ))}
      </div>
      {problem && spaces.length > 0 && (
        <footer className="border-t px-3 py-1.5 text-muted-foreground text-xs">{problem}</footer>
      )}
    </div>
  )
}

export default { views: { "work/main": Planner } } satisfies ModuleWeb
