/**
 * The bar's account menu, for an app with sign-in: who is signed in (and their role), change
 * the password, sign out. Absent when the app has no sign-in.
 */
import { type FormEvent, useState } from "react"
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
import { Icon } from "../sdk/icon"
import { useAccount } from "./gate"
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

export function AccountMenu() {
  const { account, signedOut } = useAccount()
  const [pw, setPw] = useState(false)
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
          <DropdownMenuItem onClick={() => setPw(true)}>
            <Icon name="key-round" /> Change password
          </DropdownMenuItem>
          <DropdownMenuItem onClick={out}>
            <Icon name="log-out" /> Sign out
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <PasswordDialog open={pw} onOpenChange={setPw} />
    </>
  )
}
