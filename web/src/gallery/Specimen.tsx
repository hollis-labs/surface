/** The gallery's two layout pieces: a section with a title, and one labelled specimen. */
import { createContext, type ReactNode, useContext } from "react"

/**
 * Which sections draw. In the Draft tab only the sections marked as a design job's draft
 * draw: those whose `draft` is the job given (or any marked one when no job is given).
 */
export const DraftFilter = createContext<{ only: boolean; job?: string }>({ only: false })

export function Section({
  title,
  about,
  draft,
  children,
}: {
  title: string
  about?: string
  /** The design job this section is the draft of (its id, e.g. "d_d065599e"): it shows in the Draft tab. */
  draft?: string
  children: ReactNode
}) {
  const filter = useContext(DraftFilter)
  if (filter.only && (!draft || (filter.job && draft !== filter.job))) return null
  return (
    <section className="flex flex-col gap-3" data-gallery-draft={draft}>
      <header>
        <h2 className="text-sm font-semibold">{title}</h2>
        {about && <p className="text-xs text-muted-foreground">{about}</p>}
      </header>
      <div className="grid grid-cols-1 gap-3 @2xl:grid-cols-2">{children}</div>
    </section>
  )
}

export function Specimen({ label, wide, children }: { label: string; wide?: boolean; children: ReactNode }) {
  return (
    <div
      className={`flex min-w-0 flex-col gap-2 rounded-lg border border-border bg-background p-3 ${wide ? "@2xl:col-span-2" : ""}`}
    >
      <span className="font-mono text-xs text-muted-foreground">{label}</span>
      <div className="flex min-w-0 flex-wrap items-center gap-2">{children}</div>
    </div>
  )
}
