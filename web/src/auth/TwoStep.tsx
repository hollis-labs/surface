/**
 * Two-step sign-in, the person's side: turning it on (scan the QR code, type one code, keep the
 * recovery codes), and, once on, a new set of recovery codes or turning it off (each takes a
 * code). TotpEnroll is shared with the sign-in gate, which shows it when the account's role must
 * have a second factor.
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
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Icon } from "../sdk/icon"
import { AuthError } from "./session"
import {
  beginTotp,
  confirmTotp,
  disableTotp,
  groupSecret,
  newRecoveryCodes,
  qrSrc,
  type TotpEnrollment,
  type TotpState,
  totpState,
} from "./totp"

const errText = (err: unknown) => (err instanceof AuthError ? err.message : "Something went wrong. Try again.")

/** The one-time code field: digits for the app's code, or a recovery code when allowed. */
export function CodeInput({
  id,
  value,
  onChange,
  recovery,
  autoFocus,
}: {
  id: string
  value: string
  onChange: (v: string) => void
  recovery?: boolean
  autoFocus?: boolean
}) {
  return (
    <Input
      id={id}
      autoComplete="one-time-code"
      inputMode={recovery ? "text" : "numeric"}
      autoFocus={autoFocus}
      spellCheck={false}
      placeholder={recovery ? "123456 or a recovery code" : "123456"}
      className="font-mono tracking-wider"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  )
}

/** The recovery codes, shown once, with copy and download. */
export function RecoveryCodes({
  codes,
  onDone,
  doneLabel,
}: {
  codes: string[]
  onDone: () => void
  doneLabel: string
}) {
  const [copied, setCopied] = useState(false)
  const text = codes.join("\n")
  const download = () => {
    const a = document.createElement("a")
    a.href = URL.createObjectURL(new Blob([`${text}\n`], { type: "text/plain" }))
    a.download = "recovery-codes.txt"
    a.click()
    URL.revokeObjectURL(a.href)
  }
  return (
    <div className="flex flex-col gap-3 text-sm">
      <p className="text-muted-foreground">
        Keep these somewhere safe, apart from your phone. Each one signs you in once if you lose your authenticator app.
        They are not shown again.
      </p>
      <ul className="grid grid-cols-2 gap-x-4 gap-y-1 rounded-md bg-muted p-3 font-mono text-xs">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
      <div className="flex gap-2">
        <Button
          size="sm"
          variant="outline"
          onClick={() => void navigator.clipboard?.writeText(text).then(() => setCopied(true))}
        >
          <Icon name={copied ? "check" : "copy"} /> {copied ? "Copied" : "Copy"}
        </Button>
        <Button size="sm" variant="outline" onClick={download}>
          <Icon name="download" /> Download
        </Button>
      </div>
      <Button onClick={onDone}>{doneLabel}</Button>
    </div>
  )
}

/** Turning TOTP on: QR code and secret, one code, then the recovery codes. */
export function TotpEnroll({ onDone, onCancel }: { onDone: () => void; onCancel?: () => void }) {
  const [enr, setEnr] = useState<TotpEnrollment | null>(null)
  const [code, setCode] = useState("")
  const [codes, setCodes] = useState<string[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  useEffect(() => {
    beginTotp().then(setEnr, (err: unknown) => setError(errText(err)))
  }, [])
  const confirm = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError("")
    try {
      setCodes(await confirmTotp(code.trim()))
    } catch (err) {
      setError(errText(err))
      setCode("")
    } finally {
      setBusy(false)
    }
  }
  if (codes) return <RecoveryCodes codes={codes} onDone={onDone} doneLabel="I saved them" />
  return (
    <form className="flex flex-col gap-3 text-sm" onSubmit={(e) => void confirm(e)}>
      <p className="text-muted-foreground">
        Scan this with an authenticator app (1Password, Google Authenticator, Authy, your phone's passwords app), or
        type the key into it.
      </p>
      {enr ? (
        <div className="flex items-center gap-3">
          <img
            src={qrSrc(enr.qr_svg)}
            alt="QR code for your authenticator app"
            className="size-36 shrink-0 rounded-md border bg-white p-1"
          />
          <div className="flex min-w-0 flex-col gap-1">
            <span className="text-muted-foreground text-xs">Key</span>
            <code className="break-all font-mono text-xs">{groupSecret(enr.secret)}</code>
          </div>
        </div>
      ) : (
        !error && <div className="grid h-36 place-items-center text-muted-foreground">…</div>
      )}
      <Field>
        <FieldLabel htmlFor="totp-confirm">The 6-digit code the app shows</FieldLabel>
        <CodeInput id="totp-confirm" value={code} onChange={setCode} />
        <FieldDescription>It changes every 30 seconds.</FieldDescription>
      </Field>
      {error && (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2">
        {onCancel && (
          <Button type="button" variant="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
        )}
        <Button type="submit" disabled={busy || !enr || code.trim().length < 6}>
          {busy ? "Checking…" : "Turn on"}
        </Button>
      </div>
    </form>
  )
}

type View = "status" | "enroll" | "codes" | "off" | { shown: string[] }

/** The account menu's "Two-step sign-in". */
export function TwoStepDialog({
  open,
  onOpenChange,
  onChanged,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  onChanged?: () => void
}) {
  const [st, setSt] = useState<TotpState | null>(null)
  const [view, setView] = useState<View>("status")
  const [code, setCode] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const load = useCallback(() => void totpState().then(setSt, () => setSt(null)), [])
  useEffect(() => {
    if (open) {
      load()
      setView("status")
      setCode("")
      setError("")
    }
  }, [open, load])
  const back = () => {
    load()
    setView("status")
    setCode("")
    onChanged?.()
  }
  const withCode = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError("")
    try {
      if (view === "codes") {
        setView({ shown: await newRecoveryCodes(code.trim()) })
      } else {
        await disableTotp(code.trim())
        back()
      }
    } catch (err) {
      setError(errText(err))
    } finally {
      setBusy(false)
      setCode("")
    }
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Two-step sign-in</DialogTitle>
          <DialogDescription>
            After your password, a code from an authenticator app. Signing in with a passkey skips it.
          </DialogDescription>
        </DialogHeader>
        {view === "enroll" ? (
          <TotpEnroll onDone={back} onCancel={() => setView("status")} />
        ) : typeof view === "object" ? (
          <RecoveryCodes codes={view.shown} onDone={back} doneLabel="Done" />
        ) : view === "codes" || view === "off" ? (
          <form className="flex flex-col gap-3 text-sm" onSubmit={(e) => void withCode(e)}>
            <p className="text-muted-foreground">
              {view === "codes"
                ? "A new set replaces your recovery codes; the old ones stop working."
                : "Your password alone will sign you in again."}
            </p>
            <Field>
              <FieldLabel htmlFor="totp-code">A code from your app, or a recovery code</FieldLabel>
              <CodeInput id="totp-code" value={code} onChange={setCode} recovery autoFocus />
            </Field>
            {error && (
              <p role="alert" className="text-destructive">
                {error}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setView("status")} disabled={busy}>
                Cancel
              </Button>
              <Button
                type="submit"
                variant={view === "off" ? "destructive" : "default"}
                disabled={busy || code.trim().length < 6}
              >
                {view === "codes" ? "Make new codes" : "Turn off"}
              </Button>
            </div>
          </form>
        ) : (
          <div className="flex flex-col gap-3 text-sm">
            {!st ? (
              <p className="text-muted-foreground">…</p>
            ) : st.enabled ? (
              <>
                <p className="flex items-center gap-2">
                  <Icon name="shield-check" className="text-green-600" /> On.{" "}
                  <span className="text-muted-foreground">
                    {st.recovery_codes_left} recovery code{st.recovery_codes_left === 1 ? "" : "s"} left.
                  </span>
                </p>
                <div className="flex gap-2">
                  <Button variant="outline" size="sm" onClick={() => setView("codes")}>
                    New recovery codes
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => setView("off")}>
                    Turn off
                  </Button>
                </div>
                {st.required && (
                  <p className="text-muted-foreground text-xs">
                    Your role needs a second factor: to turn this off, add a passkey first.
                  </p>
                )}
              </>
            ) : (
              <>
                <p className="text-muted-foreground">Off. Your password alone signs you in.</p>
                <Button onClick={() => setView("enroll")}>
                  <Icon name="smartphone" /> Set up an authenticator app
                </Button>
              </>
            )}
          </div>
        )}
        {view === "status" && (
          <DialogFooter>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              Close
            </Button>
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  )
}
