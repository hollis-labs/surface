/**
 * Sign-in, for an app whose surface.yaml says `auth: identity` (Substrate's identity module).
 *
 * The session is an HttpOnly cookie the page never sees. What the page does:
 * - every same-origin /api and /mcp request carries `X-Requested-With` (the server refuses a
 *   cookie-authenticated write without it: a cross-site form cannot set it), and a 401 on one
 *   means the session ended, which sends the page back to sign-in (`onSignedOut`). That is a
 *   fetch wrapper installed once, so the agent panel, the palette, cards and every module's
 *   own fetch behave the same without each knowing;
 * - `whoami` tells sign-in on (and who) from sign-in off: the identity routes do not exist
 *   on an app without it.
 */

export interface Account {
  id: string
  username: string
  display_name: string
  email?: string
  role?: string
  scopes: string[]
}

export interface SetupStatus {
  setup_required: boolean
  setup_over_http: boolean
  setup_link?: boolean
}

/** The page's answer to "who is here". */
export type Whoami = { auth: "off" } | { auth: "identity"; account: Account | null }

const SIGNED_OUT = "surface:signed-out"

/** Routes that answer 401 for a wrong password rather than an ended session. */
const ANSWERS_401 = ["/api/login", "/api/setup", "/api/password"]

let installed = false

/** Installs the fetch wrapper (idempotent). */
export function installAuthFetch(): void {
  if (installed || typeof window === "undefined") return
  installed = true
  const base = window.fetch.bind(window)
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(input instanceof Request ? input.url : String(input), window.location.href)
    const ours =
      url.origin === window.location.origin && (url.pathname.startsWith("/api/") || url.pathname.startsWith("/mcp"))
    if (!ours) return base(input, init)
    const headers = new Headers(init?.headers ?? (input instanceof Request ? input.headers : undefined))
    if (!headers.has("X-Requested-With")) headers.set("X-Requested-With", "XMLHttpRequest")
    const res = await base(input, { ...init, headers, credentials: "same-origin" })
    if (res.status === 401 && !ANSWERS_401.includes(url.pathname)) {
      window.dispatchEvent(new Event(SIGNED_OUT))
    }
    return res
  }
}

/** Calls fn when a request finds the session ended. Returns the function that stops it. */
export function onSignedOut(fn: () => void): () => void {
  window.addEventListener(SIGNED_OUT, fn)
  return () => window.removeEventListener(SIGNED_OUT, fn)
}

/** Who is here: sign-in off (no identity routes), signed out, or the account. */
export async function whoami(): Promise<Whoami> {
  const res = await fetch("/api/identity/profile")
  if (res.status === 404) return { auth: "off" }
  if (res.ok) return { auth: "identity", account: (await res.json()) as Account }
  if (res.status === 401 || res.status === 403) return { auth: "identity", account: null }
  throw new Error(`${res.status} ${res.statusText}`)
}

export class AuthError extends Error {
  constructor(
    message: string,
    readonly code: string,
    readonly status: number,
    readonly retryAfter?: number,
  ) {
    super(message)
  }
}

async function post<T>(path: string, body: unknown, method = "POST"): Promise<T> {
  const res = await fetch(path, { method, headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })
  const text = await res.text()
  let data: Record<string, unknown> = {}
  try {
    data = text ? (JSON.parse(text) as Record<string, unknown>) : {}
  } catch {
    /* plain text */
  }
  if (!res.ok) {
    const msg = String(data.message ?? data.error ?? `${res.status} ${res.statusText}`)
    const after = Number(res.headers.get("Retry-After") ?? "") || undefined
    throw new AuthError(msg, String(data.code ?? ""), res.status, after)
  }
  return data as T
}

export async function setupStatus(): Promise<SetupStatus> {
  const res = await fetch("/api/setup")
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return (await res.json()) as SetupStatus
}

export const signIn = (username: string, password: string) => post<Account>("/api/login", { username, password })

export const setUp = (input: { username: string; display_name: string; password: string; setup_token?: string }) =>
  post<Account>("/api/setup", input)

export const signOut = () => post<unknown>("/api/logout", {})

export const changePassword = (current: string, next: string) =>
  post<unknown>("/api/password", { current_password: current, new_password: next }, "PUT")

/** The setup token a setup link carries in its fragment (`/setup#t=setup_…`), or "". */
export function setupTokenFromURL(): string {
  const m = /(?:^#|&)t=(setup_[A-Za-z0-9_-]+)/.exec(window.location.hash)
  return m ? m[1] : ""
}

/** Drops the setup link from the address bar (and history) once it is spent or not wanted. */
export function clearSetupURL(): void {
  if (window.location.pathname === "/setup" || window.location.hash.includes("t=setup_")) {
    window.history.replaceState(null, "", "/")
  }
}
