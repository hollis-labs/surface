/**
 * Terminal's window: the shells on this machine as tabs. A shell keeps running when its tab's
 * window closes or the page reloads; closing the tab (x) ends it.
 */
import { citeTerminalLinks, Icon, type ModuleWeb, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Terminal } from "@/components/ui/terminal"

interface Session {
  id: string
  title: string
  cwd: string
  exited: boolean
  exit_code: number
}

/** References in the output become links with a card (stable: read once per terminal). */
const linkProviders = [citeTerminalLinks]

const socketUrl = (id: string) =>
  `${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}/api/m/terminal/sessions/${id}/ws`

function Deck({ win, desk, api }: ViewProps) {
  const [sessions, setSessions] = useState<Session[] | null>(null)
  const [active, setActive] = useState("")
  const [error, setError] = useState("")
  const made = useRef(false)

  const load = useCallback(
    () =>
      api
        .get<{ sessions: Session[] }>("sessions")
        .then((r) => {
          setSessions(r.sessions)
          setError("")
          return r.sessions
        })
        .catch((e: Error) => {
          setError(e.message)
          return null // not known: never taken as "no shells"
        }),
    [api],
  )
  const create = useCallback(
    () =>
      api
        .post<Session>("sessions", {})
        .then((s) => {
          setActive(s.id)
          return load()
        })
        .catch((e: Error) => setError(e.message)),
    [api, load],
  )
  const close = (id: string) =>
    void api
      .del(`sessions/${id}`)
      .then(load)
      .catch((e: Error) => setError(e.message))

  // The first time the window opens with no shell, it starts one.
  useEffect(() => {
    void load().then((list) => {
      if (list && list.length === 0 && !made.current) {
        made.current = true
        void create()
      }
    })
  }, [load, create])
  useEvent("terminal.changed", () => void load())
  useEffect(() => {
    if (sessions && !sessions.some((s) => s.id === active) && sessions[0]) setActive(sessions[0].id)
  }, [sessions, active])
  useEffect(() => desk.report(win.id, { kind: "data", ref: "terminal:sessions" }), [desk, win.id])

  const markExited = (id: string, code: number) =>
    setSessions((list) => list?.map((s) => (s.id === id ? { ...s, exited: true, exit_code: code } : s)) ?? null)

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex shrink-0 items-center gap-0.5 border-b px-1 py-1">
        {(sessions ?? []).map((s) => (
          <div key={s.id} className="flex min-w-0 items-center">
            <Button
              variant={s.id === active ? "secondary" : "ghost"}
              size="xs"
              className="min-w-0 max-w-48"
              aria-pressed={s.id === active}
              title={`${s.title} · ${s.cwd}`}
              onClick={() => setActive(s.id)}
            >
              <Icon name="square-terminal" />
              <span className="truncate">{s.title}</span>
              {s.exited && <span className="text-muted-foreground">(exited {s.exit_code})</span>}
            </Button>
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={`Close ${s.title}`}
              title="End this shell"
              onClick={() => close(s.id)}
            >
              <Icon name="x" />
            </Button>
          </div>
        ))}
        <Button variant="ghost" size="icon-xs" aria-label="New shell" title="New shell" onClick={() => void create()}>
          <Icon name="plus" />
        </Button>
        {error && <span className="ml-auto px-2 text-xs text-destructive">{error}</span>}
      </div>
      <div className="relative min-h-0 flex-1">
        {sessions?.length === 0 ? (
          <Empty className="h-full">
            <EmptyHeader>
              <EmptyTitle>No shell</EmptyTitle>
              <EmptyDescription>Start one with +.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          (sessions ?? []).map((s) => (
            <div key={s.id} className={s.id === active ? "absolute inset-0" : "hidden"}>
              <Terminal
                linkProviders={linkProviders}
                socketUrl={socketUrl(s.id)}
                active={s.id === active}
                onExit={(code) => markExited(s.id, code)}
              />
            </div>
          ))
        )}
      </div>
    </div>
  )
}

export default { views: { "terminal/main": Deck } } satisfies ModuleWeb
