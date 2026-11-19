/** References: the chip in each state, its card, and the hover card that holds it. From fixtures. */
import { Brain, Copy, FileText, Link2, ListChecks, Maximize2, Zap } from "lucide-react"
import { Button } from "@/components/ui/button"
import { CiteCard } from "@/components/ui/cite-card"
import { CiteChip } from "@/components/ui/cite-chip"
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card"
import { Section, Specimen } from "./Specimen"

const TASK = {
  kindLabel: "Torque task",
  canonical: "CW-20261119-0064",
  title: "Port Cite to Surface",
  subtitle: "Torque task · CW-20261119-0064",
  description:
    "References in any text become chips with a hover card and an open action, resolved by the module that owns them.",
  status: { label: "in_progress", tone: "info" as const },
  fields: [
    { label: "Priority", value: "P2" },
    { label: "Project", value: "PRJ-20260101-0001", mono: true },
    { label: "Tags", value: "surface, cite" },
  ],
  updated: "12m ago",
}

const actions = (
  <>
    <Button variant="ghost" size="xs">
      <Copy />
      Copy
    </Button>
    <Button variant="outline" size="xs">
      <Maximize2 />
      Open
    </Button>
  </>
)

export function Cite() {
  return (
    <>
      <Section title="Cite chip" about="A reference inline in text, in each state it is drawn in.">
        <Specimen label="states" wide>
          <p className="text-sm leading-relaxed">
            Found <CiteChip state="found" icon={<ListChecks />} label="Task: Port Cite to Surface" />, loading{" "}
            <CiteChip state="loading" icon={<Brain />} label="01M7…EN4Q" />, not found{" "}
            <CiteChip state="not-found" icon={<ListChecks />} label="CW-20990101-0001" />, failed{" "}
            <CiteChip state="error" icon={<Zap />} label="fragment abababab" />, not resolved here{" "}
            <CiteChip state="unsupported" icon={<Link2 />} label="example.com/docs" /> and from code{" "}
            <CiteChip state="found" mono icon={<FileText />} label="main.go:42" />.
          </p>
        </Specimen>
      </Section>
      <Section title="Cite card" about="What a reference stands for; one card for every kind.">
        <Specimen label="ready">
          <div className="w-80 rounded-lg p-2.5 ring-1 ring-foreground/10">
            <CiteCard icon={<ListChecks />} {...TASK} actions={actions} />
          </div>
        </Specimen>
        <Specimen label="loading, not found, error">
          <div className="flex w-80 flex-col gap-3">
            <div className="rounded-lg p-2.5 ring-1 ring-foreground/10">
              <CiteCard
                phase="loading"
                icon={<Brain />}
                kindLabel="Tesseract item"
                canonical="01M7T1BJ53BCQWTPC844XEEN4Q"
                title="01M7…EN4Q"
              />
            </div>
            <div className="rounded-lg p-2.5 ring-1 ring-foreground/10">
              <CiteCard
                phase="not-found"
                icon={<ListChecks />}
                kindLabel="Torque task"
                canonical="CW-20990101-0001"
                title="CW-20990101-0001"
                subtitle="Torque task not found"
              />
            </div>
            <div className="rounded-lg p-2.5 ring-1 ring-foreground/10">
              <CiteCard
                phase="error"
                icon={<Zap />}
                kindLabel="Fragment"
                canonical="abababab…"
                title="fragment abababab"
                error="Could not be looked up: the Fragments Engine could not be reached"
              />
            </div>
          </div>
        </Specimen>
      </Section>
      <Section title="Hover card" about="A card shown while the pointer rests on its trigger.">
        <Specimen label="a chip with its card">
          <p className="text-sm">
            Hover{" "}
            <HoverCard>
              <HoverCardTrigger
                render={<CiteChip state="found" icon={<ListChecks />} label="Task: Port Cite to Surface" />}
              />
              <HoverCardContent className="w-80" align="start">
                <CiteCard icon={<ListChecks />} {...TASK} actions={actions} />
              </HoverCardContent>
            </HoverCard>{" "}
            for its card.
          </p>
        </Specimen>
      </Section>
    </>
  )
}
