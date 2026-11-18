/** The gallery's two layout pieces: a section with a title, and one labelled specimen. */
import type { ReactNode } from "react"

export function Section({ title, about, children }: { title: string; about?: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
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
