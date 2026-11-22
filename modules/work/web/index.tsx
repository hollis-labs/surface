/**
 * The Work engine's windows (modules/work/README.md): Planner, Content and Writer are one
 * window opened on their preset's app. It lists the app's workspaces, opens one on its saved
 * views (board, outline, calendar, canvas, flow), and shows an item's detail beside the view.
 * Ported from Basalisk's frontend/src/work. The items live in Caster; every change is written
 * there at once through the module's routes, and the module's work.changed event refreshes.
 *
 * What is in front is kept in the window's args ({workspace, view, item}) and reported to the
 * desk, so the agent's work slot and the projects module see it: work:<ws>/<id>.
 */
import { Icon, type ModuleWeb, type SurfaceEvent, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Item, ItemActions, ItemContent, ItemDescription, ItemMedia, ItemTitle } from "@/components/ui/item"
import { Spinner } from "@/components/ui/spinner"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { type ViewArgs, type WorkClient, type WorkPreset, type WorkSpace, type Workspace, workClient } from "./api"
import { Board } from "./board"
import { Calendar } from "./calendar"
import { Canvas } from "./canvas"
import { Detail } from "./detail"
import { Flow } from "./flow"
import { Outline } from "./outline"
import { Pick } from "./parts"
import { ago, message, slug } from "./util"
import "./work.css"

/** Each window's app (its preset) and title, by view id (modules/work/views.go). */
const APPS: Record<string, { app: string; title: string; icon: string }> = {
  work: { app: "planner", title: "Planner", icon: "target" },
  "work.content": { app: "content", title: "Content", icon: "megaphone" },
  "work.writer": { app: "writer", title: "Writer", icon: "pen-line" },
}

/** The views a workspace can be given from its header. */
const NEW_VIEWS = [
  { view: "board", title: "Board", icon: "kanban", group_by: "stage" },
  // No date field: the calendar places items by the first date field the kinds have.
  { view: "calendar", title: "Calendar", icon: "calendar-days" },
  { view: "outline", title: "Outline", icon: "list-tree" },
  { view: "canvas", title: "Canvas", icon: "layout-dashboard" },
  { view: "flow", title: "Flow", icon: "workflow", rel: ["leads-to", "depends-on", "appears-in"] },
]
const viewIcon = (v: string) => NEW_VIEWS.find((x) => x.view === v)?.icon ?? "square"

interface Args {
  workspace?: string
  view?: string
  item?: string
}
type Changed = SurfaceEvent<{ workspace?: string; id?: string; what?: string }>

function WorkWindow({ win, desk, api }: ViewProps) {
  const app = APPS[win.view] ?? APPS.work
  const w = useMemo(() => workClient(api), [api])
  const args: Args = {
    workspace: typeof win.args.workspace === "string" ? win.args.workspace : undefined,
    view: typeof win.args.view === "string" ? win.args.view : undefined,
    item: typeof win.args.item === "string" ? win.args.item : undefined,
  }
  const go = useCallback(
    (next: Args) => {
      const a: Record<string, unknown> = {}
      for (const [k, v] of Object.entries(next)) if (v) a[k] = v
      void desk.setArgs(win.id, a)
    },
    [desk, win.id],
  )

  // What is in front, for the agent and the slots.
  const { workspace, view, item } = args
  useEffect(() => {
    const ref = !workspace ? "work" : item ? `work:${workspace}/${item}` : `work:${workspace}`
    desk.report(win.id, {
      kind: item ? "item" : "workspace",
      ref,
      app: app.app,
      ...(workspace ? { workspace } : {}),
      ...(view ? { view } : {}),
      ...(item ? { item } : {}),
    })
  }, [desk, win.id, app.app, workspace, view, item])

  return (
    <div className="work-root flex h-full min-h-0 flex-col">
      {workspace ? (
        <SpaceView
          key={workspace}
          w={w}
          desk={desk}
          winId={win.id}
          viewId={win.view}
          appTitle={app.title}
          args={{ ...args, workspace }}
          go={go}
        />
      ) : (
        <Workspaces w={w} desk={desk} winId={win.id} viewId={win.view} app={app} open={(ws) => go({ workspace: ws })} />
      )}
    </div>
  )
}

// ---- The app's workspaces ----

function Workspaces({
  w,
  desk,
  winId,
  viewId,
  app,
  open,
}: {
  w: WorkClient
  desk: ViewProps["desk"]
  winId: string
  viewId: string
  app: { app: string; title: string; icon: string }
  open: (ws: string) => void
}) {
  const [list, setList] = useState<Workspace[]>([])
  const [presets, setPresets] = useState<WorkPreset[]>([])
  const [problem, setProblem] = useState("")
  const [error, setError] = useState("")
  const [loaded, setLoaded] = useState(false)
  const [making, setMaking] = useState(false)

  const load = useCallback(
    async (refresh = false) => {
      try {
        const r = await w.list(app.app, refresh)
        setList(r.workspaces)
        setPresets(r.presets.filter((p) => p.app === app.app))
        setProblem(r.problem ?? "")
        setError("")
      } catch (e) {
        setError(message(e))
      } finally {
        setLoaded(true)
      }
    },
    [w, app.app],
  )
  useEffect(() => {
    void load(true)
  }, [load])
  useEvent("work.changed", () => void load())
  useEffect(() => void desk.setTitle(winId, app.title), [desk, winId, app.title])

  return (
    <>
      <header className="flex shrink-0 items-center gap-2 border-b px-3 py-2">
        <Icon name={app.icon} size={15} className="text-muted-foreground" />
        <span className="font-medium text-sm">{app.title}</span>
        <span className="ml-auto" />
        <Button
          variant="ghost"
          size="icon-sm"
          title="Read again from Caster"
          aria-label="Refresh"
          onClick={() => void load(true)}
        >
          <Icon name="refresh-cw" />
        </Button>
        <Button variant="outline" size="sm" onClick={() => setMaking((m) => !m)}>
          <Icon name="plus" /> New workspace
        </Button>
      </header>
      {making && (
        <NewWorkspace
          w={w}
          presets={presets}
          done={(ws) => {
            setMaking(false)
            if (ws) open(ws)
          }}
        />
      )}
      {(error || problem) && (
        <p className="shrink-0 border-b px-3 py-1.5 text-warning text-xs" role="status">
          {error || `Showing the copy kept here: ${problem}`}
        </p>
      )}
      <div className="min-h-0 flex-1 overflow-auto p-2">
        {!loaded && <Spinner className="m-4" />}
        {loaded && list.length === 0 && !error && (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>No {app.title} workspaces yet</EmptyTitle>
              <EmptyDescription>
                A workspace is one body of work: a channel, a book, a launch. Make one with New workspace; it is kept in
                Caster.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        )}
        <div className="grid grid-cols-[repeat(auto-fill,minmax(16rem,1fr))] gap-2">
          {list.map((s) => (
            <Item
              key={s.slug}
              variant="outline"
              size="sm"
              className="group cursor-pointer"
              onClick={(e) => {
                if (e.metaKey || e.ctrlKey || e.shiftKey) {
                  void desk.open(viewId, {
                    key: `${viewId}:${s.slug}:${Date.now()}`,
                    title: `${app.title}: ${s.title}`,
                    args: { workspace: s.slug },
                  })
                } else {
                  open(s.slug)
                }
              }}
              title={`caster://work/${s.slug} (⌘-click to open in new window)`}
            >
              <ItemMedia>
                <Icon name={app.icon} size={14} className="text-muted-foreground" />
              </ItemMedia>
              <ItemContent>
                <ItemTitle>
                  {s.title}
                  {s.archived && <Badge variant="secondary">archived</Badge>}
                </ItemTitle>
                <ItemDescription>
                  {s.problem ||
                    `${s.items} item${s.items === 1 ? "" : "s"} · ${ago(s.updated)}${s.body.trim() ? ` · ${s.body.trim().split("\n")[0]}` : ""}`}
                </ItemDescription>
              </ItemContent>
              <ItemActions>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  className="opacity-0 transition-opacity group-hover:opacity-100"
                  title="Open workspace in new window"
                  aria-label={`Open ${s.title} in new window`}
                  onClick={(e) => {
                    e.stopPropagation()
                    void desk.open(viewId, {
                      key: `${viewId}:${s.slug}:${Date.now()}`,
                      title: `${app.title}: ${s.title}`,
                      args: { workspace: s.slug },
                    })
                  }}
                >
                  <Icon name="external-link" />
                </Button>
                <Icon name="chevron-right" size={14} className="text-muted-foreground" />
              </ItemActions>
            </Item>
          ))}
        </div>
      </div>
    </>
  )
}

function NewWorkspace({ w, presets, done }: { w: WorkClient; presets: WorkPreset[]; done: (ws?: string) => void }) {
  const [title, setTitle] = useState("")
  const [name, setName] = useState("")
  const [about, setAbout] = useState("")
  const [preset, setPreset] = useState(presets[0]?.id ?? "")
  const templates = presets.find((p) => p.id === preset)?.templates ?? []
  const [template, setTemplate] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!preset && presets[0]) setPreset(presets[0].id)
  }, [preset, presets])
  const save = async () => {
    setBusy(true)
    try {
      const r = await w.create(slug(name || title), title.trim(), preset, about, template)
      if (r.problem) setError(r.problem)
      done(r.workspace)
    } catch (e) {
      setError(message(e))
    } finally {
      setBusy(false)
    }
  }
  const ok = !busy && !!preset && !!(name.trim() || title.trim())
  return (
    <div
      className="flex shrink-0 flex-wrap items-center gap-2 border-b bg-muted/40 px-3 py-2"
      onKeyDown={(e) => {
        if (e.key === "Enter" && ok) void save()
        if (e.key === "Escape") done()
      }}
    >
      <Input
        autoFocus
        className="h-8 w-48"
        placeholder="Title"
        value={title}
        onChange={(e) => setTitle(e.target.value)}
      />
      <Input
        className="h-8 w-44"
        placeholder={`Name (${slug(title) || "my-channel"})`}
        value={name}
        onChange={(e) => setName(e.target.value)}
      />
      {presets.length > 1 && (
        <Pick
          label="Start from"
          value={preset}
          onChange={setPreset}
          options={presets.map((p) => ({ value: p.id, label: p.label }))}
        />
      )}
      {templates.length > 0 && (
        <Pick
          label="Start from a template"
          value={template}
          onChange={setTemplate}
          options={[{ value: "", label: "Empty" }, ...templates.map((t) => ({ value: t.id, label: t.label }))]}
        />
      )}
      <Input
        className="h-8 min-w-40 flex-1"
        placeholder="What it is (optional)"
        value={about}
        onChange={(e) => setAbout(e.target.value)}
      />
      <Button size="sm" disabled={!ok} onClick={() => void save()}>
        Make it
      </Button>
      <Button variant="ghost" size="sm" onClick={() => done()}>
        Cancel
      </Button>
      {error && <span className="text-warning text-xs">{error}</span>}
    </div>
  )
}

// ---- A workspace opened ----

function SpaceView({
  w,
  desk,
  winId,
  viewId,
  appTitle,
  args,
  go,
}: {
  w: WorkClient
  desk: ViewProps["desk"]
  winId: string
  viewId: string
  appTitle: string
  args: Args & { workspace: string }
  go: (a: Args) => void
}) {
  const ws = args.workspace
  const [sp, setSp] = useState<WorkSpace | null>(null)
  const [problem, setProblem] = useState("")
  // What went wrong reading the workspace, and what went wrong doing something in it: a
  // read-back must not wipe the second.
  const [loadError, setLoadError] = useState("")
  const [error, setError] = useState("")
  const [showCut, setShowCut] = useState(false)
  // A section being edited in the detail: hold read-backs until it is kept or dropped.
  const editing = useRef(false)

  const load = useCallback(
    async (refresh = false) => {
      try {
        const r = await w.open(ws, refresh)
        setSp(r.space)
        setProblem(r.problem ?? "")
        setLoadError("")
      } catch (e) {
        setLoadError(message(e))
      }
    },
    [w, ws],
  )
  useEffect(() => {
    void load(true)
  }, [load])
  // The agent, another window or this one changed something in this workspace.
  useEvent("work.changed", (ev: Changed) => {
    if (ev.data?.workspace && ev.data.workspace !== ws) return
    if (!editing.current) void load()
  })
  useEffect(() => {
    if (sp) void desk.setTitle(winId, `${appTitle}: ${sp.workspace.title}`)
  }, [desk, winId, appTitle, sp])

  const kinds = useMemo(() => new Map((sp?.kinds ?? []).map((k) => [k.kind, k])), [sp])
  const view =
    sp?.views.find((v) => v.id === args.view) ?? sp?.views.find((v) => v.id === sp.default_view) ?? sp?.views[0] ?? null
  const picked = args.item ?? null
  const pick = (id: string | null, newWindow = false) => {
    if (newWindow && id) {
      const card = sp?.items.find((x) => x.id === id)
      void desk.open(viewId, {
        key: `${viewId}:${ws}:${id}:${Date.now()}`,
        title: `${appTitle}: ${card?.title || id}`,
        args: { workspace: ws, view: args.view, item: id },
      })
      return
    }
    go({ workspace: ws, view: args.view, item: id ?? undefined })
  }
  const back = () => go({})

  const header = (body: React.ReactNode) => (
    <header className="flex shrink-0 items-center gap-2 border-b px-2 py-1.5">
      <Button variant="ghost" size="icon-sm" onClick={back} title="All workspaces" aria-label="All workspaces">
        <Icon name="arrow-left" />
      </Button>
      {body}
    </header>
  )
  if (!sp) return header(<span className="text-muted-foreground text-sm">{loadError || <Spinner />}</span>)

  const cut = sp.items.filter((x) => x.cut)
  const va: ViewArgs = { w, ws, space: sp, picked, pick, changed: () => void load(), setError }
  return (
    <>
      {header(
        <>
          <span className="truncate font-medium text-sm" title={`caster://work/${sp.workspace.slug}`}>
            {sp.workspace.title}
          </span>
          <Tabs value={view?.id ?? ""} onValueChange={(v) => go({ workspace: ws, view: String(v), item: args.item })}>
            <TabsList>
              {sp.views.map((v) => (
                <TabsTrigger key={v.id} value={v.id}>
                  <Icon name={viewIcon(v.view)} />
                  {v.title || v.view}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon-sm" title="Add a view" aria-label="Add a view">
                  <Icon name="plus" />
                </Button>
              }
            />
            <DropdownMenuContent align="start" className="w-44">
              <DropdownMenuLabel>Add a view</DropdownMenuLabel>
              {NEW_VIEWS.map((nv) => (
                <DropdownMenuItem
                  key={nv.view}
                  onClick={async () => {
                    try {
                      // A flow is about links across kinds: it starts with every kind.
                      const kindsShown = nv.view === "flow" ? [] : (sp.views[0]?.kinds ?? [])
                      const { icon: _, ...set } = nv
                      const r = await w.newView(ws, { ...set, kinds: kindsShown })
                      go({ workspace: ws, view: r.id, item: args.item })
                    } catch (e) {
                      setError(message(e))
                    }
                    void load()
                  }}
                >
                  <Icon name={nv.icon} />
                  {nv.title}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
          <span className="ml-auto" />
          {cut.length > 0 && (
            <Button
              variant={showCut ? "secondary" : "ghost"}
              size="sm"
              onClick={() => setShowCut((c) => !c)}
              title="Items taken out of the views; they stay in Caster"
            >
              <Icon name="scissors" /> {cut.length} cut
            </Button>
          )}
          <Button
            variant="ghost"
            size="icon-sm"
            title="Read again from Caster"
            aria-label="Refresh"
            onClick={() => void load(true)}
          >
            <Icon name="refresh-cw" />
          </Button>
        </>,
      )}
      {(error || loadError || problem) && (
        <div className="flex shrink-0 items-center gap-2 border-b px-3 py-1.5 text-warning text-xs" role="status">
          {error || loadError || `Showing the copy kept here: ${problem}`}
          {error && (
            <Button
              variant="ghost"
              size="icon-xs"
              className="ml-auto"
              title="Dismiss"
              aria-label="Dismiss"
              onClick={() => setError("")}
            >
              <Icon name="x" />
            </Button>
          )}
        </div>
      )}
      <div className="flex min-h-0 flex-1">
        <div className="flex min-w-0 flex-1 flex-col">
          {showCut && (
            <div className="flex max-h-40 shrink-0 flex-col gap-1 overflow-y-auto border-b p-2">
              {cut.map((x) => (
                <div key={x.id} className="flex items-center gap-2 text-sm">
                  <Button variant="link" size="xs" onClick={() => pick(x.id)}>
                    {kinds.get(x.kind)?.label ?? x.kind} · {x.title}
                  </Button>
                  <Button
                    variant="outline"
                    size="xs"
                    className="ml-auto"
                    onClick={async () => {
                      try {
                        await w.restore(ws, x.id)
                      } catch (e) {
                        setError(message(e))
                      }
                      void load()
                    }}
                  >
                    Restore
                  </Button>
                </div>
              ))}
            </div>
          )}
          {view?.view === "board" || !view ? (
            <Board {...va} view={view} />
          ) : view.view === "flow" ? (
            <Flow key={view.id} {...va} view={view} />
          ) : view.view === "canvas" ? (
            <Canvas key={view.id} {...va} view={view} />
          ) : view.view === "outline" ? (
            <Outline {...va} view={view} />
          ) : view.view === "calendar" ? (
            <Calendar {...va} view={view} />
          ) : (
            <p className="p-4 text-muted-foreground text-sm">
              This workspace has a {view.view} view, which is not known here.
            </p>
          )}
        </div>
        {picked && (
          <Detail
            key={picked}
            w={w}
            desk={desk}
            ws={ws}
            id={picked}
            kinds={kinds}
            items={sp.items}
            revOnBoard={sp.items.find((x) => x.id === picked)?.rev ?? 0}
            close={() => pick(null)}
            open={(id, newWin) => pick(id, newWin)}
            popOut={() => pick(picked, true)}
            editing={(on) => {
              editing.current = on
            }}
            changed={() => void load()}
          />
        )}
      </div>
    </>
  )
}

export default { views: { "work/main": WorkWindow } } satisfies ModuleWeb
