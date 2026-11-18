import { call } from "./runtime"
import type { Mark } from "./types"

/** Pin a note of the owner's to a part of something. ref is what WindowContext.ref names. */
export const addMark = (m: { ref: string; body: string; quote?: string; anchor?: unknown }) =>
  call<Mark>("POST", "/api/marks", m)

export const replyMark = (id: string, body: string) =>
  call<Mark>("POST", `/api/marks/${encodeURIComponent(id)}/replies`, { body })

export const resolveMark = (id: string) =>
  call<void>("POST", `/api/marks/${encodeURIComponent(id)}/resolve`)
