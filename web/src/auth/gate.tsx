/**
 * The sign-in gate: for an app with sign-in, nothing of the desk loads until someone has signed
 * in. It follows the identity contract @hollis-labs/kit-identity uses (setup, sign-in,
 * offline setup, expired session), drawn with the app's own registry components so it wears
 * the app's theme.
 *
 * First run: the owner opens the setup link `<app> identity setup-link` printed
 * (`/setup#t=setup_…`). Without a link (and without the development-only open setup) the gate
 * says how to make one; it never offers a form that the server would refuse.
 */

import { KeyRound } from "lucide-react"
import {
  createContext,
  type FormEvent,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react"
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { addPasskey, cancelled, deviceName, passkeysUsable, signInWithPasskey } from "./passkeys"
import {
  type Account,
  AuthError,
  acceptInvite,
  checkInvite,
  clearInviteURL,
  clearSetupURL,
  type InviteInfo,
  installAuthFetch,
  inviteCodeFromURL,
  onSignedOut,
  type SetupStatus,
  setUp,
  setupStatus,
  setupTokenFromURL,
  signIn,
  whoami,
} from "./session"

interface AccountState {
  /** The signed-in account; null when the app has no sign-in. */
  account: Account | null
  /** Re-reads the account (after a profile change). */
  refresh: () => void
  /** Back to the sign-in screen (after signing out). */
  signedOut: () => void
}

const AccountContext = createContext<AccountState>({ account: null, refresh: () => {}, signedOut: () => {} })

/** The signed-in account and the gate's controls; account is null when sign-in is off. */
export function useAccount(): AccountState {
  return useContext(AccountContext)
}

type GateState =
  | { k: "loading" }
  | { k: "error"; message: string }
  | { k: "off" }
  | { k: "in"; account: Account }
  | { k: "setup"; token: string; passkeys: boolean }
  | { k: "offer-passkey" }
  | { k: "invite"; code: string; info: InviteInfo; passkeys: boolean }
  | { k: "invite-invalid"; message: string }
  | { k: "setup-offline" }
  | { k: "sign-in"; expired: boolean; notice?: string; passkeys: boolean }

/** Wraps the page: with sign-in, children mount only for a signed-in person. */
export function AuthGate({ name, title, children }: { name: string; title: string; children: ReactNode }) {
  const [state, setState] = useState<GateState>({ k: "loading" })
  const signedIn = useRef(false)

  const check = useCallback(async (expired = false) => {
    try {
      const who = await whoami()
      if (who.auth === "off") {
        setState({ k: "off" })
        return
      }
      if (who.account) {
        signedIn.current = true
        clearSetupURL()
        clearInviteURL()
        setState({ k: "in", account: who.account })
        return
      }
      const st: SetupStatus = await setupStatus()
      const invite = inviteCodeFromURL()
      if (invite && !st.setup_required) {
        try {
          setState({
            k: "invite",
            code: invite,
            info: await checkInvite(invite),
            passkeys: passkeysUsable(st.passkeys),
          })
        } catch (err) {
          clearInviteURL()
          setState({ k: "invite-invalid", message: (err as Error).message })
        }
        return
      }
      const token = setupTokenFromURL()
      if (st.setup_required) {
        setState(
          token || st.setup_over_http
            ? { k: "setup", token, passkeys: passkeysUsable(st.passkeys) }
            : { k: "setup-offline" },
        )
        return
      }
      clearSetupURL()
      setState({ k: "sign-in", expired, passkeys: passkeysUsable(st.passkeys) })
    } catch (err) {
      setState({ k: "error", message: (err as Error).message })
    }
  }, [])

  useEffect(() => {
    installAuthFetch()
    void check()
    return onSignedOut(() => {
      if (signedIn.current) {
        signedIn.current = false
        void check(true)
      }
    })
  }, [check])

  const signedOut = useCallback(() => {
    signedIn.current = false
    void check()
  }, [check])
  const refresh = useCallback(() => void check(), [check])

  switch (state.k) {
    case "loading":
      return <div className="grid h-full place-items-center text-muted-foreground">…</div>
    case "error":
      return (
        <Screen title={title} heading="Unable to start">
          <p className="text-destructive text-sm">The app's server did not answer: {state.message}</p>
          <Button variant="outline" onClick={() => void check()}>
            Try again
          </Button>
        </Screen>
      )
    case "off":
      return <AccountContext.Provider value={{ account: null, refresh, signedOut }}>{children}</AccountContext.Provider>
    case "in":
      return (
        <AccountContext.Provider value={{ account: state.account, refresh, signedOut }}>
          {children}
        </AccountContext.Provider>
      )
    case "setup":
      return (
        <SetupScreen
          title={title}
          token={state.token}
          done={() => (state.passkeys ? setState({ k: "offer-passkey" }) : void check())}
          closed={() => void check()}
        />
      )
    case "invite":
      return (
        <InviteScreen
          title={title}
          code={state.code}
          info={state.info}
          done={() => (state.passkeys ? setState({ k: "offer-passkey" }) : void check())}
        />
      )
    case "invite-invalid":
      return (
        <Screen title={title} heading="This invite does not work">
          <p className="text-muted-foreground text-sm">{state.message}</p>
          <Button variant="outline" onClick={() => void check()}>
            Go to sign in
          </Button>
        </Screen>
      )
    case "offer-passkey":
      return <OfferPasskeyScreen title={title} done={() => void check()} />
    case "setup-offline":
      return <SetupOfflineScreen name={name} title={title} />
    case "sign-in":
      return (
        <SignInScreen
          title={title}
          expired={state.expired}
          notice={state.notice}
          passkeys={state.passkeys}
          done={() => void check()}
        />
      )
  }
}

function Screen({ title, heading, children }: { title: string; heading: string; children: ReactNode }) {
  return (
    <div className="grid h-full place-items-center bg-background p-4">
      <div className="w-full max-w-sm">
        <div className="mb-4 text-center text-muted-foreground text-sm">{title}</div>
        <div className="flex flex-col gap-4 rounded-xl border bg-card p-6 text-card-foreground shadow-sm">
          <h1 className="font-semibold text-lg">{heading}</h1>
          {children}
        </div>
      </div>
    </div>
  )
}

function message(err: unknown): string {
  if (err instanceof AuthError) {
    if (err.status === 429 && err.retryAfter) {
      const at = new Date(Date.now() + err.retryAfter * 1000)
      return `Too many attempts. Try again in ${Math.ceil(err.retryAfter / 60)} min (at ${at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}).`
    }
    return err.message
  }
  return "Something went wrong. Try again."
}

function ErrorBox({ text }: { text: string }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (text) ref.current?.focus()
  }, [text])
  if (!text) return null
  return (
    <div
      ref={ref}
      tabIndex={-1}
      role="alert"
      className="rounded-md bg-destructive/10 px-3 py-2 text-destructive text-sm"
    >
      {text}
    </div>
  )
}

function SignInScreen({
  title,
  expired,
  notice,
  passkeys,
  done,
}: {
  title: string
  expired: boolean
  notice?: string
  passkeys: boolean
  done: () => void
}) {
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError("")
    try {
      await signIn(username.trim(), password)
      done()
    } catch (err) {
      setError(
        err instanceof AuthError && err.code === "passkey_required"
          ? "This account signs in with a passkey only. Use “Sign in with a passkey”."
          : message(err),
      )
      setPassword("")
    } finally {
      setBusy(false)
    }
  }
  const withPasskey = async () => {
    setBusy(true)
    setError("")
    try {
      await signInWithPasskey()
      done()
    } catch (err) {
      if (!cancelled(err)) setError(message(err))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Screen title={title} heading="Sign in">
      {expired && <p className="text-muted-foreground text-sm">Your session ended. Sign in again.</p>}
      {notice && <p className="text-muted-foreground text-sm">{notice}</p>}
      {passkeys && (
        <>
          <Button variant="outline" disabled={busy} onClick={() => void withPasskey()}>
            <KeyRound /> Sign in with a passkey
          </Button>
          <div className="flex items-center gap-2 text-muted-foreground text-xs">
            <span className="h-px flex-1 bg-border" /> or with a password <span className="h-px flex-1 bg-border" />
          </div>
        </>
      )}
      <form onSubmit={(e) => void submit(e)}>
        <FieldGroup>
          <fieldset disabled={busy} className="contents">
            <Field>
              <FieldLabel htmlFor="auth-username">Username</FieldLabel>
              <Input
                id="auth-username"
                autoComplete="username"
                autoFocus
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="auth-password">Password</FieldLabel>
              <Input
                id="auth-password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </Field>
          </fieldset>
          <ErrorBox text={error} />
          <Button type="submit" disabled={busy || !username.trim() || !password}>
            {busy ? "Signing in…" : "Sign in"}
          </Button>
        </FieldGroup>
      </form>
    </Screen>
  )
}

/** Right after setup: offer a passkey, so the next sign-in needs no password. */
function OfferPasskeyScreen({ title, done }: { title: string; done: () => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const add = async () => {
    setBusy(true)
    setError("")
    try {
      await addPasskey(deviceName())
      done()
    } catch (err) {
      if (!cancelled(err)) setError(message(err))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Screen title={title} heading="Add a passkey">
      <p className="text-muted-foreground text-sm">
        Sign in next time with this device's fingerprint, face or screen lock instead of your password. You can add more
        passkeys, or make your account passkey-only, from the account menu.
      </p>
      <ErrorBox text={error} />
      <Button disabled={busy} onClick={() => void add()}>
        <KeyRound /> {busy ? "Waiting for your device…" : "Add a passkey"}
      </Button>
      <Button variant="ghost" disabled={busy} onClick={done}>
        Not now
      </Button>
    </Screen>
  )
}

const ROLE_TEXT: Record<string, string> = {
  owner: "an owner",
  admin: "an admin",
  member: "a member",
  guest: "a guest (read only)",
}

/** An invite link: choose a username and password and join with the invite's role. */
function InviteScreen({
  title,
  code,
  info,
  done,
}: {
  title: string
  code: string
  info: InviteInfo
  done: () => void
}) {
  const [display, setDisplay] = useState(info.label ?? "")
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [again, setAgain] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const mismatch = again !== "" && again !== password
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (password !== again) return
    setBusy(true)
    setError("")
    try {
      await acceptInvite({ code, username: username.trim(), display_name: display.trim(), password })
      clearInviteURL()
      done()
    } catch (err) {
      setError(message(err))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Screen title={title} heading={`Join ${title}`}>
      <p className="text-muted-foreground text-sm">
        You were invited as {ROLE_TEXT[info.role] ?? info.role}. Choose how you will sign in. The invite works once.
      </p>
      <form onSubmit={(e) => void submit(e)}>
        <FieldGroup>
          <fieldset disabled={busy} className="contents">
            <Field>
              <FieldLabel htmlFor="inv-display">Your name</FieldLabel>
              <Input
                id="inv-display"
                autoComplete="name"
                autoFocus
                value={display}
                onChange={(e) => setDisplay(e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="inv-username">Username</FieldLabel>
              <Input
                id="inv-username"
                autoComplete="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
              <FieldDescription>Lowercase letters, digits, dot, dash or underscore.</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="inv-password">Password</FieldLabel>
              <Input
                id="inv-password"
                type="password"
                autoComplete="new-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              <FieldDescription>At least 12 characters. You can add a passkey once you are in.</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="inv-again">Repeat the password</FieldLabel>
              <Input
                id="inv-again"
                type="password"
                autoComplete="new-password"
                value={again}
                onChange={(e) => setAgain(e.target.value)}
                aria-invalid={mismatch}
              />
              {mismatch && (
                <FieldDescription className="text-destructive">The passwords do not match.</FieldDescription>
              )}
            </Field>
          </fieldset>
          <ErrorBox text={error} />
          <Button type="submit" disabled={busy || !username.trim() || password.length < 12 || password !== again}>
            {busy ? "Joining…" : "Join and sign in"}
          </Button>
        </FieldGroup>
      </form>
    </Screen>
  )
}

function SetupScreen({
  title,
  token,
  done,
  closed,
}: {
  title: string
  token: string
  done: () => void
  closed: () => void
}) {
  const [display, setDisplay] = useState("")
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [again, setAgain] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const mismatch = again !== "" && again !== password
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (password !== again) return
    setBusy(true)
    setError("")
    try {
      await setUp({
        display_name: display.trim(),
        username: username.trim(),
        password,
        setup_token: token || undefined,
      })
      clearSetupURL()
      done()
    } catch (err) {
      if (err instanceof AuthError && err.code === "setup_closed") {
        clearSetupURL()
        closed()
        return
      }
      setError(message(err))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Screen title={title} heading="Create the owner account">
      <p className="text-muted-foreground text-sm">
        This is the first account, with full control of {title}. You can invite others once you are in.
      </p>
      <form onSubmit={(e) => void submit(e)}>
        <FieldGroup>
          <fieldset disabled={busy} className="contents">
            <Field>
              <FieldLabel htmlFor="setup-display">Your name</FieldLabel>
              <Input
                id="setup-display"
                autoComplete="name"
                autoFocus
                value={display}
                onChange={(e) => setDisplay(e.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="setup-username">Username</FieldLabel>
              <Input
                id="setup-username"
                autoComplete="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
              <FieldDescription>Lowercase letters, digits, dot, dash or underscore.</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="setup-password">Password</FieldLabel>
              <Input
                id="setup-password"
                type="password"
                autoComplete="new-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              <FieldDescription>At least 12 characters.</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="setup-again">Repeat the password</FieldLabel>
              <Input
                id="setup-again"
                type="password"
                autoComplete="new-password"
                value={again}
                onChange={(e) => setAgain(e.target.value)}
                aria-invalid={mismatch}
              />
              {mismatch && (
                <FieldDescription className="text-destructive">The passwords do not match.</FieldDescription>
              )}
            </Field>
          </fieldset>
          <ErrorBox text={error} />
          <Button type="submit" disabled={busy || !username.trim() || password.length < 12 || password !== again}>
            {busy ? "Creating…" : "Create account and sign in"}
          </Button>
        </FieldGroup>
      </form>
    </Screen>
  )
}

function SetupOfflineScreen({ name, title }: { name: string; title: string }) {
  const cmd = `${name} identity setup-link --url ${window.location.origin}`
  return (
    <Screen title={title} heading="Set up the owner on the host">
      <p className="text-muted-foreground text-sm">
        This app has no owner yet. For safety the first account cannot be created from an open page. On the machine that
        runs it, make a one-time setup link and open it here:
      </p>
      <pre className="whitespace-pre-wrap break-all rounded-md bg-muted p-3 font-mono text-xs">{cmd}</pre>
      <p className="text-muted-foreground text-xs">The link works once and expires after 30 minutes.</p>
    </Screen>
  )
}
