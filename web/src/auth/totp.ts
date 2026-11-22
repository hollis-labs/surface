/**
 * Two-step sign-in (TOTP: authenticator-app codes) against Substrate's identity routes. Turning
 * it on is two calls: begin (a secret and its QR code) and confirm (one code from the app,
 * which answers the recovery codes, shown once).
 */
import { AuthError } from "./session"

async function call<T>(path: string, body?: unknown, method = "POST"): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let data: Record<string, unknown> = {}
  try {
    data = text ? (JSON.parse(text) as Record<string, unknown>) : {}
  } catch {
    /* plain text */
  }
  if (!res.ok) {
    const after = Number(res.headers.get("Retry-After") ?? "") || undefined
    throw new AuthError(
      String(data.message ?? `${res.status} ${res.statusText}`),
      String(data.code ?? ""),
      res.status,
      after,
    )
  }
  return data as T
}

export interface TotpState {
  enabled: boolean
  pending: boolean
  recovery_codes_left: number
  /** The account's role must have a second factor. */
  required: boolean
}

export interface TotpEnrollment {
  secret: string
  uri: string
  /** The otpauth link as an SVG QR code. */
  qr_svg: string
}

export const totpState = () => call<TotpState>("/api/identity/totp", undefined, "GET")

export const beginTotp = () => call<TotpEnrollment>("/api/identity/totp/begin", {})

export const confirmTotp = (code: string) =>
  call<{ recovery_codes: string[] }>("/api/identity/totp/confirm", { code }).then((r) => r.recovery_codes)

export const newRecoveryCodes = (code: string) =>
  call<{ recovery_codes: string[] }>("/api/identity/totp/recovery-codes", { code }).then((r) => r.recovery_codes)

export const disableTotp = (code: string) => call<unknown>("/api/identity/totp", { code }, "DELETE")

/** The QR code as an image source (it is the server's SVG; an <img> keeps it inert). */
export const qrSrc = (svg: string) => `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`

/** The secret in groups of four, easier to type. */
export const groupSecret = (s: string) => s.replace(/(.{4})/g, "$1 ").trim()
