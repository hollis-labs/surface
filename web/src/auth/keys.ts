/**
 * API keys against Substrate's identity routes: a person's own keys (/api/identity/me/keys) and,
 * for an administrator, everyone's (/api/identity/keys). A key is shown once, when it is made.
 */
import { AuthError } from "./session"

export interface ApiKey {
  id: string
  name: string
  scopes: string[]
  created_by?: string
  created_at: string
  expires_at?: string
  last_used_at?: string
}

async function call<T>(path: string, body?: unknown, method = "GET"): Promise<T> {
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
    throw new AuthError(String(data.message ?? `${res.status} ${res.statusText}`), String(data.code ?? ""), res.status)
  }
  return data as T
}

export const myKeys = () => call<{ keys: ApiKey[] | null }>("/api/identity/me/keys").then((r) => r.keys ?? [])

export const createMyKey = (name: string, scopes: string[], ttlHours: number) =>
  call<{ key: string; credential: ApiKey }>("/api/identity/me/keys", { name, scopes, ttl_hours: ttlHours }, "POST")

export const revokeMyKey = (id: string) =>
  call<unknown>(`/api/identity/me/keys/${encodeURIComponent(id)}`, undefined, "DELETE")

/** Every key (administrators). */
export const allKeys = () => call<{ keys: ApiKey[] | null }>("/api/identity/keys").then((r) => r.keys ?? [])

export const revokeAnyKey = (id: string) =>
  call<unknown>(`/api/identity/keys/${encodeURIComponent(id)}`, undefined, "DELETE")

/** The access presets: scopes for an app named app. */
export function accessPresets(app: string): { id: string; label: string; scopes: string[] }[] {
  return [
    { id: "use", label: "Read and act", scopes: [`${app}.use`, `${app}.read`] },
    { id: "read", label: "Read only", scopes: [`${app}.read`] },
  ]
}

/** What a key's scopes amount to, in words. */
export function accessLabel(app: string, scopes: string[]): string {
  if (scopes.includes("*")) return "everything"
  if (scopes.includes(`${app}.admin`)) return "admin"
  if (scopes.includes(`${app}.use`)) return "use"
  if (scopes.includes(`${app}.read`)) return "read only"
  return scopes.join(" ")
}

export const EXPIRY = [
  { id: "720", label: "30 days", hours: 720 },
  { id: "2160", label: "90 days", hours: 2160 },
  { id: "8760", label: "1 year", hours: 8760 },
  { id: "0", label: "Never", hours: 0 },
]
