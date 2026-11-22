/**
 * Users and invites, for an administrator: who has an account and with which role (change it,
 * disable an account), the pending invites (revoke one), and a new invite for a role, shown as a
 * link to send. Everything goes through Substrate's identity routes, which also enforce the
 * rules (nobody grants more than they hold; the last owner stays an owner).
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
  type Account,
  AuthError,
  createInvite,
  inviteLink,
  listInvites,
  listUsers,
  type PendingInvite,
  revokeInvite,
  type UserRow,
  updateUser,
} from "./session"

const ROLES = ["owner", "admin", "member", "guest"]

function RoleSelect({
  value,
  onChange,
  disabled,
}: {
  value: string
  onChange: (r: string) => void
  disabled?: boolean
}) {
  return (
    <Select value={value} onValueChange={(v) => v && onChange(String(v))} disabled={disabled}>
      <SelectTrigger size="sm" className="w-28">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {ROLES.map((r) => (
          <SelectItem key={r} value={r}>
            {r}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

export function UsersDialog({
  me,
  open,
  onOpenChange,
}: {
  me: Account
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const [users, setUsers] = useState<UserRow[]>([])
  const [invites, setInvites] = useState<PendingInvite[]>([])
  const [role, setRole] = useState("member")
  const [label, setLabel] = useState("")
  const [link, setLink] = useState("")
  const [copied, setCopied] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  const load = useCallback(() => {
    void listUsers().then(setUsers, () => {})
    void listInvites().then(setInvites, () => {})
  }, [])
  useEffect(() => {
    if (open) {
      load()
      setLink("")
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
  const invite = () =>
    run(async () => {
      const inv = await createInvite(role, label.trim())
      setLink(inviteLink(inv.code))
      setCopied(false)
      setLabel("")
    })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Users and invites</DialogTitle>
          <DialogDescription>
            Everyone here shares this app. Roles: owner and admin manage it; members use it; guests look.
          </DialogDescription>
        </DialogHeader>
        <div className="flex max-h-[60vh] flex-col gap-4 overflow-auto text-sm">
          <section className="flex flex-col gap-1.5">
            <h3 className="font-medium">People</h3>
            {users.map((u) => (
              <div key={u.id} className="flex items-center gap-2 rounded-md border px-3 py-1.5">
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className={`truncate ${u.disabled ? "text-muted-foreground line-through" : ""}`}>
                    {u.display_name || u.username}
                    {u.id === me.id ? " (you)" : ""}
                  </span>
                  <span className="truncate text-muted-foreground text-xs">{u.username}</span>
                </div>
                <RoleSelect
                  value={u.role || "member"}
                  disabled={busy}
                  onChange={(r) => void run(() => updateUser(u.id, { role: r }))}
                />
                {u.id !== me.id && (
                  <Button
                    size="xs"
                    variant="ghost"
                    disabled={busy}
                    onClick={() => void run(() => updateUser(u.id, { disabled: !u.disabled }))}
                  >
                    {u.disabled ? "Enable" : "Disable"}
                  </Button>
                )}
              </div>
            ))}
          </section>

          <section className="flex flex-col gap-1.5">
            <h3 className="font-medium">Invite someone</h3>
            <div className="flex gap-2">
              <Input placeholder="Who is it for? (optional)" value={label} onChange={(e) => setLabel(e.target.value)} />
              <RoleSelect value={role} onChange={setRole} disabled={busy} />
              <Button disabled={busy} onClick={() => void invite()}>
                <Icon name="user-plus" /> Invite
              </Button>
            </div>
            {link && (
              <div className="flex flex-col gap-1 rounded-md bg-muted p-2">
                <span className="text-muted-foreground text-xs">Send this link. It works once, for 7 days.</span>
                <div className="flex items-center gap-2">
                  <code className="min-w-0 flex-1 break-all font-mono text-xs">{link}</code>
                  <Button
                    size="xs"
                    variant="outline"
                    onClick={() => void navigator.clipboard?.writeText(link).then(() => setCopied(true))}
                  >
                    <Icon name={copied ? "check" : "copy"} /> {copied ? "Copied" : "Copy"}
                  </Button>
                </div>
              </div>
            )}
            {invites.length > 0 && (
              <div className="flex flex-col gap-1">
                <span className="text-muted-foreground text-xs">Pending</span>
                {invites.map((i) => (
                  <div key={i.id} className="flex items-center gap-2 rounded-md border px-3 py-1.5">
                    <span className="flex-1 truncate">{i.label || "Invite"}</span>
                    <span className="text-muted-foreground text-xs">
                      {i.role} · until {new Date(i.expires_at).toLocaleDateString()}
                    </span>
                    <Button
                      size="xs"
                      variant="ghost"
                      disabled={busy}
                      onClick={() => void run(() => revokeInvite(i.id))}
                    >
                      Revoke
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </section>
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
