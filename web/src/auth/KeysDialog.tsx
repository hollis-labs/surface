/**
 * API keys, for the signed-in person: their own keys (for an agent or a script), a new one (a name,
 * what it may do, when it expires; the secret is shown once to copy), and revoking. An
 * administrator also sees everyone's keys and can revoke them.
 */
import { useCallback, useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Icon } from "../sdk/icon"
import {
  type ApiKey,
  accessLabel,
  accessPresets,
  allKeys,
  createMyKey,
  EXPIRY,
  myKeys,
  revokeAnyKey,
  revokeMyKey,
} from "./keys"
import { type Account, AuthError, isAdmin, listUsers } from "./session"

const day = (s?: string) => (s && !s.startsWith("0001") ? new Date(s).toLocaleDateString() : "")

function KeyRow({
  k,
  app,
  owner,
  busy,
  onRevoke,
}: {
  k: ApiKey
  app: string
  owner?: string
  busy: boolean
  onRevoke: () => void
}) {
  const expires = day(k.expires_at)
  const expired = Boolean(expires) && new Date(k.expires_at as string) < new Date()
  return (
    <div className="flex items-center gap-2 rounded-md border px-3 py-1.5">
      <Icon name="key-round" size={14} className="shrink-0" />
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate">
          {k.name}
          {owner ? <span className="text-muted-foreground"> · {owner}</span> : null}
        </span>
        <span className="truncate text-muted-foreground text-xs">
          {accessLabel(app, k.scopes)} · made {day(k.created_at)} ·{" "}
          {day(k.last_used_at) ? `used ${day(k.last_used_at)}` : "not used yet"} ·{" "}
          {expires ? (expired ? `expired ${expires}` : `expires ${expires}`) : "no expiry"}
        </span>
      </div>
      <Button size="xs" variant="ghost" disabled={busy} onClick={onRevoke}>
        Revoke
      </Button>
    </div>
  )
}

export function KeysDialog({
  me,
  app,
  open,
  onOpenChange,
}: {
  me: Account
  app: string
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const admin = isAdmin(me)
  const presets = accessPresets(app)
  const [mine, setMine] = useState<ApiKey[]>([])
  const [others, setOthers] = useState<ApiKey[]>([])
  const [names, setNames] = useState<Record<string, string>>({})
  const [name, setName] = useState("")
  const [preset, setPreset] = useState(presets[0].id)
  const [expiry, setExpiry] = useState(EXPIRY[1].id)
  const [secret, setSecret] = useState("")
  const [copied, setCopied] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  const load = useCallback(() => {
    void myKeys().then(setMine, () => {})
    if (admin) {
      void allKeys().then(
        (all) => setOthers(all.filter((k) => k.created_by !== `user:${me.id}`)),
        () => {},
      )
      void listUsers().then(
        (us) => setNames(Object.fromEntries(us.map((u) => [`user:${u.id}`, u.display_name || u.username]))),
        () => {},
      )
    }
  }, [admin, me.id])
  useEffect(() => {
    if (open) {
      load()
      setSecret("")
      setError("")
    }
  }, [open, load])

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    setError("")
    try {
      await fn()
      load()
    } catch (err) {
      setError(err instanceof AuthError ? err.message : "Something went wrong.")
    } finally {
      setBusy(false)
    }
  }
  const create = () =>
    run(async () => {
      const scopes = presets.find((p) => p.id === preset)?.scopes ?? presets[0].scopes
      const hours = EXPIRY.find((e) => e.id === expiry)?.hours ?? 720
      const made = await createMyKey(name.trim(), scopes, hours)
      setSecret(made.key)
      setCopied(false)
      setName("")
    })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>API keys</DialogTitle>
          <DialogDescription>
            For your agents and scripts: a key acts as you, with the access you give it, never more than you have.
          </DialogDescription>
        </DialogHeader>
        <div className="flex max-h-[60vh] flex-col gap-4 overflow-auto text-sm">
          <section className="flex flex-col gap-1.5">
            <h3 className="font-medium">New key</h3>
            <div className="flex flex-wrap gap-2">
              <Input
                className="min-w-40 flex-1"
                placeholder="Name, e.g. laptop agent"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
              <Select value={preset} onValueChange={(v) => v && setPreset(String(v))}>
                <SelectTrigger size="sm" className="w-40">
                  <SelectValue>{presets.find((p) => p.id === preset)?.label}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {presets.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={expiry} onValueChange={(v) => v && setExpiry(String(v))}>
                <SelectTrigger size="sm" className="w-28">
                  <SelectValue>{EXPIRY.find((e) => e.id === expiry)?.label}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {EXPIRY.map((e) => (
                    <SelectItem key={e.id} value={e.id}>
                      {e.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button disabled={busy || !name.trim()} onClick={() => void create()}>
                <Icon name="plus" /> Create
              </Button>
            </div>
            {secret && (
              <div className="flex flex-col gap-1 rounded-md bg-muted p-2">
                <span className="text-muted-foreground text-xs">
                  Copy it now: it is not shown again. Anyone with it can act as you, within its access.
                </span>
                <div className="flex items-center gap-2">
                  <code className="min-w-0 flex-1 break-all font-mono text-xs">{secret}</code>
                  <Button
                    size="xs"
                    variant="outline"
                    onClick={() => void navigator.clipboard?.writeText(secret).then(() => setCopied(true))}
                  >
                    <Icon name={copied ? "check" : "copy"} /> {copied ? "Copied" : "Copy"}
                  </Button>
                </div>
              </div>
            )}
          </section>

          <section className="flex flex-col gap-1.5">
            <h3 className="font-medium">Your keys</h3>
            {mine.length === 0 && <p className="text-muted-foreground">None yet.</p>}
            {mine.map((k) => (
              <KeyRow key={k.id} k={k} app={app} busy={busy} onRevoke={() => void run(() => revokeMyKey(k.id))} />
            ))}
          </section>

          {admin && others.length > 0 && (
            <section className="flex flex-col gap-1.5">
              <h3 className="font-medium">Everyone else's</h3>
              {others.map((k) => (
                <KeyRow
                  key={k.id}
                  k={k}
                  app={app}
                  owner={k.created_by ? (names[k.created_by] ?? k.created_by) : "the app"}
                  busy={busy}
                  onRevoke={() => void run(() => revokeAnyKey(k.id))}
                />
              ))}
            </section>
          )}
          {error && <p className="text-destructive">{error}</p>}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
