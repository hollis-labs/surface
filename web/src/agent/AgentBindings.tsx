import { useCallback, useEffect, useId, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useEvent } from "../sdk"
import { setSetting } from "../sdk/runtime"
import { agentApi, type BindingInfo } from "./api"

/** Shared folder editor for Settings and the panel's Profile picker. */
export function AgentBindings() {
  const [bindings, setBindings] = useState<BindingInfo[]>([])
  const [error, setError] = useState("")
  const [saving, setSaving] = useState(false)
  const refresh = useCallback(() => {
    agentApi.profiles().then(
      (s) => setBindings(s.bindings ?? []),
      (e: Error) => setError(e.message),
    )
  }, [])
  useEffect(refresh, [refresh])
  useEvent("config.changed", refresh)
  const save = async (id: string, dir: string) => {
    setError("")
    setSaving(true)
    try {
      // Read the latest mapping before editing one module, preserving other bindings.
      const state = await agentApi.profiles()
      const next = Object.fromEntries(
        (state.bindings ?? []).filter((b) => b.dir && b.id !== id).map((b) => [b.id, b.dir]),
      )
      if (dir.trim()) next[id] = dir.trim()
      await setSetting("agent.bindings", next)
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }
  return (
    <section className="flex flex-col gap-3" aria-label="Module agents">
      <p className="text-sm text-muted-foreground">
        Choose an agent folder for each module. Reset restores its built-in profile or the app’s agent.
      </p>
      {bindings.map((b) => (
        <BindingRow key={`${b.id}:${b.dir ?? ""}`} binding={b} disabled={saving} save={save} />
      ))}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </section>
  )
}

function BindingRow({
  binding: b,
  disabled,
  save,
}: {
  binding: BindingInfo
  disabled: boolean
  save: (id: string, dir: string) => Promise<void>
}) {
  const [dir, setDir] = useState(b.dir ?? "")
  const inputId = useId()
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={inputId}>
        {b.title}
        {b.name ? ` — ${b.name}` : ""}
      </Label>
      <div className="flex flex-wrap items-center gap-2">
        <Input
          id={inputId}
          className="min-w-48 flex-1"
          value={dir}
          placeholder="~/.config/my-app/agents/my-agent"
          onChange={(e) => setDir(e.target.value)}
          disabled={disabled}
        />
        <Button
          size="sm"
          disabled={disabled || !dir.trim() || dir === (b.dir ?? "")}
          onClick={() => void save(b.id, dir)}
        >
          Use folder
        </Button>
        {b.dir && (
          <Button size="sm" variant="outline" disabled={disabled} onClick={() => void save(b.id, "")}>
            Reset
          </Button>
        )}
      </div>
      {b.dir && (
        <p className="text-sm text-muted-foreground">
          Uses the app’s model, compaction, token budget and tools.
          {b.ignored_runtime?.length ? ` Ignored host.yaml runtime keys: ${b.ignored_runtime.join(", ")}.` : ""}
        </p>
      )}
      {b.error && (
        <p role="alert" className="text-sm text-destructive">
          {b.error}
        </p>
      )}
    </div>
  )
}
