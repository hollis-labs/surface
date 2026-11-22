/**
 * Passkeys (WebAuthn) against Substrate's identity routes. The server sends the options as
 * JSON (binary fields base64url) and wants the browser's answer back the same way; browsers that
 * have PublicKeyCredential.parse*OptionsFromJSON / toJSON do the conversion themselves, and the
 * small fallback below does it for the rest.
 */
import { AuthError } from "./session"

/** Passkeys can work here: the server offers them and this page is a secure context. */
export function passkeysUsable(serverOffers: boolean | undefined): boolean {
  return (
    Boolean(serverOffers) && typeof window !== "undefined" && window.isSecureContext && "PublicKeyCredential" in window
  )
}

const b64urlToBuf = (s: string): ArrayBuffer => {
  const pad = "=".repeat((4 - (s.length % 4)) % 4)
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/") + pad)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out.buffer
}

const bufToB64url = (b: ArrayBuffer | null | undefined): string | undefined => {
  if (!b) return undefined
  const bytes = new Uint8Array(b)
  let s = ""
  for (const x of bytes) s += String.fromCharCode(x)
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")
}

type JSONOptions = { publicKey: Record<string, unknown> } | Record<string, unknown>

const unwrap = (o: JSONOptions): Record<string, unknown> =>
  ("publicKey" in o ? (o.publicKey as Record<string, unknown>) : o) as Record<string, unknown>

type Descriptor = { id: string; type: string; transports?: string[] }

function creationOptions(o: JSONOptions): PublicKeyCredentialCreationOptions {
  const pk = unwrap(o)
  const PKC = window.PublicKeyCredential as unknown as {
    parseCreationOptionsFromJSON?: (j: unknown) => PublicKeyCredentialCreationOptions
  }
  if (PKC.parseCreationOptionsFromJSON) return PKC.parseCreationOptionsFromJSON(pk)
  const user = pk.user as { id: string; name: string; displayName: string }
  return {
    ...(pk as object),
    challenge: b64urlToBuf(pk.challenge as string),
    user: { ...user, id: b64urlToBuf(user.id) },
    excludeCredentials: ((pk.excludeCredentials as Descriptor[] | undefined) ?? []).map((c) => ({
      ...c,
      id: b64urlToBuf(c.id),
    })),
  } as PublicKeyCredentialCreationOptions
}

function requestOptions(o: JSONOptions): PublicKeyCredentialRequestOptions {
  const pk = unwrap(o)
  const PKC = window.PublicKeyCredential as unknown as {
    parseRequestOptionsFromJSON?: (j: unknown) => PublicKeyCredentialRequestOptions
  }
  if (PKC.parseRequestOptionsFromJSON) return PKC.parseRequestOptionsFromJSON(pk)
  return {
    ...(pk as object),
    challenge: b64urlToBuf(pk.challenge as string),
    allowCredentials: ((pk.allowCredentials as Descriptor[] | undefined) ?? []).map((c) => ({
      ...c,
      id: b64urlToBuf(c.id),
    })),
  } as PublicKeyCredentialRequestOptions
}

/** The browser's credential as the JSON the server parses. */
function credentialJSON(c: PublicKeyCredential): unknown {
  const withJSON = c as PublicKeyCredential & { toJSON?: () => unknown }
  if (typeof withJSON.toJSON === "function") return withJSON.toJSON()
  const r = c.response as AuthenticatorResponse & {
    attestationObject?: ArrayBuffer
    authenticatorData?: ArrayBuffer
    signature?: ArrayBuffer
    userHandle?: ArrayBuffer | null
    getTransports?: () => string[]
  }
  return {
    id: c.id,
    rawId: bufToB64url(c.rawId),
    type: c.type,
    authenticatorAttachment: (c as PublicKeyCredential & { authenticatorAttachment?: string }).authenticatorAttachment,
    clientExtensionResults: c.getClientExtensionResults(),
    response: {
      clientDataJSON: bufToB64url(r.clientDataJSON),
      attestationObject: bufToB64url(r.attestationObject),
      transports: r.getTransports?.(),
      authenticatorData: bufToB64url(r.authenticatorData),
      signature: bufToB64url(r.signature),
      userHandle: bufToB64url(r.userHandle ?? undefined),
    },
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
    throw new AuthError(String(data.message ?? data.error ?? res.statusText), String(data.code ?? ""), res.status)
  }
  return data as T
}

interface Begun {
  challenge_id: string
  options: JSONOptions
}

/** A browser that the person dismissed or that timed out: not worth an error message. */
export function cancelled(err: unknown): boolean {
  return err instanceof DOMException && (err.name === "NotAllowedError" || err.name === "AbortError")
}

/** Signs in with a passkey (no username): the session cookie is set by the server. */
export async function signInWithPasskey(): Promise<void> {
  const b = await post<Begun>("/api/passkey/begin", {})
  const cred = (await navigator.credentials.get({ publicKey: requestOptions(b.options) })) as PublicKeyCredential | null
  if (!cred) throw new DOMException("no credential", "NotAllowedError")
  await post("/api/passkey/finish", { challenge_id: b.challenge_id, credential: credentialJSON(cred) })
}

/** Adds a passkey to the signed-in account. */
export async function addPasskey(name: string): Promise<void> {
  const b = await post<Begun>("/api/identity/passkeys/register/begin", {})
  const cred = (await navigator.credentials.create({
    publicKey: creationOptions(b.options),
  })) as PublicKeyCredential | null
  if (!cred) throw new DOMException("no credential", "NotAllowedError")
  await post("/api/identity/passkeys/register/finish", {
    challenge_id: b.challenge_id,
    credential: credentialJSON(cred),
    name,
  })
}

export interface PasskeyInfo {
  id: string
  name: string
  created_at: string
  last_used_at?: string
}

export interface PasskeyState {
  passkeys: PasskeyInfo[]
  passkey_only: boolean
  enabled: boolean
}

export async function passkeyState(): Promise<PasskeyState> {
  const res = await fetch("/api/identity/passkeys")
  if (!res.ok) return { passkeys: [], passkey_only: false, enabled: false }
  return (await res.json()) as PasskeyState
}

export const removePasskey = (id: string) => post(`/api/identity/passkeys/${encodeURIComponent(id)}`, {}, "DELETE")

export const setPasskeyOnly = (enabled: boolean) => post("/api/identity/passkey-only", { enabled }, "PUT")

/** A default name for a new passkey: the platform, e.g. "Mac", "iPhone", "Windows". */
export function deviceName(): string {
  const ua = navigator.userAgent
  if (/iPhone/.test(ua)) return "iPhone"
  if (/iPad/.test(ua)) return "iPad"
  if (/Android/.test(ua)) return "Android"
  if (/Mac/.test(ua)) return "Mac"
  if (/Windows/.test(ua)) return "Windows"
  if (/Linux/.test(ua)) return "Linux"
  return "Passkey"
}
