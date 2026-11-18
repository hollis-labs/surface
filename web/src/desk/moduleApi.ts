import type { ModuleAPI } from "../sdk"
import { call } from "../sdk/runtime"

const cache = new Map<string, ModuleAPI>()

/** Calls to one module's own routes (/api/m/<id>/...). */
export function moduleApi(id: string): ModuleAPI {
  const hit = cache.get(id)
  if (hit) return hit
  const base = `/api/m/${id}/`
  const url = (path: string) => base + path.replace(/^\//, "")
  const api: ModuleAPI = {
    get: (p) => call("GET", url(p)),
    post: (p, body) => call("POST", url(p), body ?? {}),
    put: (p, body) => call("PUT", url(p), body ?? {}),
    del: (p) => call("DELETE", url(p)),
    url,
  }
  cache.set(id, api)
  return api
}
