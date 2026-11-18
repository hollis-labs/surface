import { icons, type LucideProps } from "lucide-react"

const pascal = (name: string) =>
  name
    .split("-")
    .map((p) => p.charAt(0).toUpperCase() + p.slice(1))
    .join("")

/** A lucide icon by the name a manifest gives it ("sticky-note"). Unknown names draw a dot. */
export function Icon({ name, ...props }: { name?: string } & LucideProps) {
  const Cmp = name ? (icons as Record<string, React.ComponentType<LucideProps>>)[pascal(name)] : undefined
  if (!Cmp) return <icons.Dot {...props} />
  return <Cmp {...props} />
}
