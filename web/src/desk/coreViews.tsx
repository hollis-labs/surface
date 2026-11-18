/** The core's own windows: Settings, Layouts and Marks. They are views like any module's. */
import { useCallback, useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Icon, type Mark, type ModuleWeb, replyMark, resolveMark, useEvent, type ViewProps } from "../sdk"
import { allSettings, call, loadSettings, type SettingEntry, setSetting } from "../sdk/runtime"
import { app, deleteLayout, deskState, type LayoutDef, listLayouts, loadLayout, saveLayout } from "./store"

function Settings(_: ViewProps) {
  const [entries, setEntries] = useState<SettingEntry[]>(allSettings())
  const [error, setError] = useState("")
  const refresh = useCallback(() => void loadSettings().then(setEntries), [])
  useEffect(refresh, [refresh])
  // A change made anywhere (another window, the agent, the CLI) shows here at once.
  useEvent("config.changed", refresh)

  const change = (e: SettingEntry, value: unknown) => {
    setError("")
    setSetting(e.full_key, value).catch((err: Error) => setError(`${e.title}: ${err.message}`))
  }
  const reset = (e: SettingEntry) =>
    void call("DELETE", `/api/settings/${encodeURIComponent(e.full_key)}`).then(refresh)

  const scopes = [...new Set(entries.map((e) => e.scope))]
  const title = (scope: string) =>
    scope === "surface" ? "Desk" : (app().modules.find((m) => m.id === scope)?.title ?? scope)
  return (
    <div className="flex flex-col gap-3 p-4">
      <p className="text-muted-foreground">
        Settings are kept in the app's database and take effect as you change them. Nothing restarts.
      </p>
      {error && <p className="text-destructive">{error}</p>}
      {scopes.map((scope) => (
        <section key={scope} className="flex flex-col">
          <h3 className="pt-2 pb-1.5 font-medium text-muted-foreground text-xs uppercase tracking-wider">
            {title(scope)}
          </h3>
          {entries
            .filter((e) => e.scope === scope)
            .map((e) => (
              <div key={e.full_key} className="flex items-center justify-between gap-4 border-t py-2.5">
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="font-medium">{e.title}</span>
                  {e.about && <span className="text-muted-foreground text-xs">{e.about}</span>}
                  <code className="font-mono text-muted-foreground text-xs">{e.full_key}</code>
                </span>
                <span className="flex shrink-0 items-center gap-2">
                  {e.type === "bool" && (
                    <Switch checked={!!e.value} onCheckedChange={(on) => change(e, on)} aria-label={e.title} />
                  )}
                  {e.type === "enum" && (
                    <Select value={String(e.value)} onValueChange={(v) => change(e, v)}>
                      <SelectTrigger size="sm" aria-label={e.title}>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {e.options?.map((o) => (
                          <SelectItem key={o} value={o}>
                            {o}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )}
                  {e.type === "int" && (
                    <Input
                      className="w-24"
                      type="number"
                      aria-label={e.title}
                      value={Number(e.value)}
                      onChange={(ev) => {
                        const n = Number.parseInt(ev.target.value, 10)
                        if (Number.isFinite(n)) change(e, n)
                      }}
                    />
                  )}
                  {e.type === "string" && (
                    <Input
                      className="w-64"
                      aria-label={e.title}
                      defaultValue={String(e.value ?? "")}
                      key={String(e.value)}
                      onBlur={(ev) => {
                        if (ev.target.value !== e.value) change(e, ev.target.value)
                      }}
                    />
                  )}
                  {e.set && (
                    <Button variant="link" size="xs" title="Back to the default" onClick={() => reset(e)}>
                      reset
                    </Button>
                  )}
                </span>
              </div>
            ))}
        </section>
      ))}
    </div>
  )
}

function Layouts(_: ViewProps) {
  const [layouts, setLayouts] = useState<LayoutDef[]>([])
  const [name, setName] = useState("")
  const [error, setError] = useState("")
  const refresh = useCallback(() => void listLayouts().then(setLayouts), [])
  useEffect(refresh, [refresh])
  useEvent("desk.layouts", refresh)
  const active = deskState().active

  const save = (n: string) => {
    setError("")
    saveLayout(n)
      .then(() => setName(""))
      .catch((err: Error) => setError(err.message))
  }
  return (
    <div className="flex flex-col gap-3 p-4">
      <p className="text-muted-foreground">
        A layout is an arrangement you can come back to: which windows are open, how they sit, and the setup that goes
        with them (your mode, the agent's standing instructions, the reflexes that are on).
      </p>
      <form
        className="flex items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (name.trim()) save(name.trim())
        }}
      >
        <Input
          className="flex-1"
          placeholder="Save the desk as it is now, as…"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <Button type="submit" disabled={!name.trim()}>
          Save
        </Button>
      </form>
      {error && <p className="text-destructive">{error}</p>}
      {layouts.length === 0 && <p className="text-muted-foreground text-xs">No saved layouts yet.</p>}
      {layouts.map((l) => (
        <div
          key={l.name}
          className="flex flex-col gap-2 rounded-lg border bg-card p-3 data-[on=true]:border-ring"
          data-on={l.name === active}
        >
          <div className="flex items-center gap-2">
            <Icon name="layout-dashboard" size={15} />
            <strong>{l.name}</strong>
            {l.name === active && <Badge variant="secondary">in use</Badge>}
            <span className="flex-1" />
            <Button variant="outline" size="sm" onClick={() => void loadLayout(l.name)}>
              Arrange
            </Button>
            <Button
              variant="outline"
              size="sm"
              title="Replace it with the desk as it is now"
              onClick={() => save(l.name)}
            >
              Update
            </Button>
            <Button variant="destructive" size="sm" onClick={() => void deleteLayout(l.name).then(refresh)}>
              Delete
            </Button>
          </div>
          {l.about && <p className="text-muted-foreground">{l.about}</p>}
          <p className="text-muted-foreground text-xs">
            {l.mode} · {l.windows.map((w) => w.title || w.view).join(", ") || "no windows"}
          </p>
          {l.setup && Object.keys(l.setup).length > 0 && (
            <dl className="grid gap-1 text-xs [&_dd]:text-muted-foreground [&_div]:flex [&_div]:gap-2 [&_dt]:w-24 [&_dt]:shrink-0 [&_dt]:text-muted-foreground">
              {Object.entries(l.setup).map(([k, v]) => (
                <div key={k}>
                  <dt>{k}</dt>
                  <dd>{typeof v === "string" ? v : JSON.stringify(v)}</dd>
                </div>
              ))}
            </dl>
          )}
        </div>
      ))}
    </div>
  )
}

function Marks(_: ViewProps) {
  const [marks, setMarks] = useState<Mark[]>([])
  const refresh = useCallback(() => void call<Mark[]>("GET", "/api/marks").then(setMarks), [])
  useEffect(refresh, [refresh])
  useEvent("marks.changed", refresh)
  return (
    <div className="flex flex-col gap-3 p-4">
      <p className="text-muted-foreground">
        Marks are notes pinned to a part of something, by you or the agent. These are the open ones, across every
        module.
      </p>
      {marks.length === 0 && <p className="text-muted-foreground text-xs">No open marks.</p>}
      {marks.map((m) => (
        <MarkCard key={m.id} mark={m} />
      ))}
    </div>
  )
}

function MarkCard({ mark }: { mark: Mark }) {
  const [reply, setReply] = useState("")
  return (
    <div
      className="flex flex-col gap-2 rounded-lg border bg-card p-3 data-[author=agent]:border-l-2 data-[author=agent]:border-l-primary"
      data-author={mark.author}
    >
      <div className="flex items-center gap-2">
        <Icon name={mark.author === "agent" ? "bot" : "user"} size={14} />
        <code className="truncate font-mono text-muted-foreground text-xs" title={mark.ref}>
          {mark.ref}
        </code>
        <span className="flex-1" />
        <Button variant="link" size="xs" onClick={() => void resolveMark(mark.id)}>
          resolve
        </Button>
      </div>
      {mark.quote && <blockquote className="border-l-2 pl-2.5 text-muted-foreground italic">{mark.quote}</blockquote>}
      <p>{mark.body}</p>
      {mark.replies?.map((r) => (
        <p key={r.created + r.body} className="flex items-center gap-1.5 pl-2 text-muted-foreground">
          <Icon name={r.author === "agent" ? "bot" : "user"} size={12} /> {r.body}
        </p>
      ))}
      <form
        className="flex items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (reply.trim()) void replyMark(mark.id, reply.trim()).then(() => setReply(""))
        }}
      >
        <Input className="flex-1" placeholder="Reply…" value={reply} onChange={(e) => setReply(e.target.value)} />
      </form>
    </div>
  )
}

export const coreWeb: ModuleWeb = {
  views: { "surface/settings": Settings, "surface/layouts": Layouts, "surface/marks": Marks },
}
