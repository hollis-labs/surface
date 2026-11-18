import { useEffect, useState } from "react"
import { setConfirm } from "./store"

interface Question {
  title: string
  message?: string
  confirmLabel?: string
  danger?: boolean
  resolve: (yes: boolean) => void
}

/** The desk's yes/no dialog. Views reach it through desk.confirm. */
export function ConfirmHost() {
  const [q, setQ] = useState<Question | null>(null)
  useEffect(() => {
    setConfirm((question) => new Promise<boolean>((resolve) => setQ({ ...question, resolve })))
  }, [])
  useEffect(() => {
    if (!q) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") answer(false)
      if (e.key === "Enter") answer(true)
    }
    window.addEventListener("keydown", onKey, true)
    return () => window.removeEventListener("keydown", onKey, true)
  })
  if (!q) return null
  const answer = (yes: boolean) => {
    q.resolve(yes)
    setQ(null)
  }
  return (
    <div className="sf-overlay" onClick={() => answer(false)}>
      <div className="sf-dialog" role="alertdialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
        <h2>{q.title}</h2>
        {q.message && <p>{q.message}</p>}
        <div className="sf-dialog-btns">
          <button type="button" className="sf-btn" onClick={() => answer(false)}>
            Cancel
          </button>
          <button type="button" className="sf-btn" data-kind={q.danger ? "danger" : "primary"} autoFocus onClick={() => answer(true)}>
            {q.confirmLabel ?? "OK"}
          </button>
        </div>
      </div>
    </div>
  )
}
