/**
 * The bar's account menu, for an app with sign-in: who is signed in (and their role), change
 * the password, sign out. Absent when the app has no sign-in.
 */
import { type FormEvent, useCallback, useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Icon } from "../sdk/icon"
import { useAccount } from "./gate"
import {
  addPasskey,
  cancelled,
  deviceName,
  type PasskeyState,
  passkeyState,
  passkeysUsable,
  removePasskey,
  setPasskeyOnly,
} from "./passkeys"
import { AuthError, changePassword, signOut } from "./session"

function PasswordDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [again, setAgain] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [done, setDone] = useState(false)
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError("")
    try {
      await changePassword(current, next)
      setDone(true)
      setCurrent("")
      setNext("")
      setAgain("")
    } catch (err) {
      setError(err instanceof AuthError ? err.message : "Something went wrong.")
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        onOpenChange(o)
        if (!o) setDone(false)
      }}
    >
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Change password</DialogTitle>
          <DialogDescription>Your other sessions are signed out; this one stays.</DialogDescription>
        </DialogHeader>
        {done ? (
          <p className="text-sm">Your password is changed.</p>
        ) : (
          <form id="password-form" onSubmit={(e) => void submit(e)}>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="pw-current">Current password</FieldLabel>
                <Input
                  id="pw-current"
                  type="password"
                  autoComplete="current-password"
                  value={current}
                  onChange={(e) => setCurrent(e.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="pw-new">New password</FieldLabel>
                <Input
                  id="pw-new"
                  type="password"
                  autoComplete="new-password"
                  value={next}
                  onChange={(e) => setNext(e.target.value)}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="pw-again">Repeat the new password</FieldLabel>
                <Input
                  id="pw-again"
                  type="password"
                  autoComplete="new-password"
                  value={again}
                  onChange={(e) => setAgain(e.target.value)}
                />
              </Field>
              {error && <p className="text-destructive text-sm">{error}</p>}
            </FieldGroup>
          </form>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {done ? "Close" : "Cancel"}
          </Button>
          {!done && (
            <Button
              type="submit"
              form="password-form"
              disabled={busy || !current || next.length < 12 || next !== again}
            >
              {busy ? "Saving…" : "Change password"}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PasskeysDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const [st, setSt] = useState<PasskeyState | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const load = useCallback(() => void passkeyState().then(setSt), [])
  useEffect(() => {
    if (open) load()
  }, [open, load])
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    setError("")
    try {
      await fn()
      load()
    } catch (err) {
      if (!cancelled(err)) setError(err instanceof AuthError ? err.message : "Something went wrong.")
    } finally {
      setBusy(false)
    }
  }
  const usable = passkeysUsable(st?.enabled)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Passkeys</DialogTitle>
          <DialogDescription>
            Sign in with your device's fingerprint, face or screen lock instead of a password.
          </DialogDescription>
        </DialogHeader>
        {!usable ? (
          <p className="text-muted-foreground text-sm">
            Passkeys need this app on HTTPS (or localhost). Ask whoever runs it to serve it over HTTPS.
          </p>
        ) : (
          <div className="flex flex-col gap-2 text-sm">
            {(st?.passkeys ?? []).length === 0 && <p className="text-muted-foreground">No passkeys yet.</p>}
            {(st?.passkeys ?? []).map((p) => (
              <div key={p.id} className="flex items-center gap-2 rounded-md border px-3 py-2">
                <Icon name="key-round" size={14} />
                <span className="flex-1 truncate">{p.name}</span>
                <span className="text-muted-foreground text-xs">
                  {p.last_used_at ? `used ${new Date(p.last_used_at).toLocaleDateString()}` : "not used yet"}
                </span>
                <Button
                  size="icon-xs"
                  variant="ghost"
                  title="Remove"
                  disabled={busy}
                  onClick={() => void run(() => removePasskey(p.id))}
                >
                  <Icon name="trash" />
                </Button>
              </div>
            ))}
            <Button variant="outline" disabled={busy} onClick={() => void run(() => addPasskey(deviceName()))}>
              <Icon name="plus" /> {busy ? "Waiting for your device…" : "Add a passkey"}
            </Button>
            {(st?.passkeys ?? []).length > 0 && (
              <div className="mt-1 flex items-start gap-2 text-sm">
                <Switch
                  id="passkey-only"
                  className="mt-0.5"
                  checked={Boolean(st?.passkey_only)}
                  disabled={busy}
                  onCheckedChange={(c: boolean) => void run(() => setPasskeyOnly(c))}
                />
                <label htmlFor="passkey-only">
                  Passkey-only
                  <span className="block text-muted-foreground text-xs">
                    Your password stops working. Keep a second passkey (another device) in case you lose this one.
                  </span>
                </label>
              </div>
            )}
            {error && <p className="text-destructive">{error}</p>}
          </div>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function AccountMenu() {
  const { account, signedOut } = useAccount()
  const [pw, setPw] = useState(false)
  const [pk, setPk] = useState(false)
  if (!account) return null
  const out = () => void signOut().finally(signedOut)
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="xs"
              className="h-5.5 gap-1 rounded-sm px-1.5"
              title={`Signed in as ${account.username}`}
            >
              <Icon name="circle-user" size={14} />
              <span className="max-w-32 truncate">{account.display_name || account.username}</span>
            </Button>
          }
        />
        <DropdownMenuContent align="end" className="w-56">
          <DropdownMenuGroup>
            <DropdownMenuLabel>
              <div className="font-medium">{account.display_name || account.username}</div>
              <div className="font-normal text-muted-foreground text-xs">
                {account.username}
                {account.role ? ` · ${account.role}` : ""}
              </div>
            </DropdownMenuLabel>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => setPk(true)}>
            <Icon name="fingerprint" /> Passkeys
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => setPw(true)}>
            <Icon name="key-round" /> Change password
          </DropdownMenuItem>
          <DropdownMenuItem onClick={out}>
            <Icon name="log-out" /> Sign out
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <PasswordDialog open={pw} onOpenChange={setPw} />
      <PasskeysDialog open={pk} onOpenChange={setPk} />
    </>
  )
}
