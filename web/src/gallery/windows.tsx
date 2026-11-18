/** The window manager with a small fixed tree: two tiled stacks, one with two tabs, and a float. */
import { FileText, Globe, StickyNote } from "lucide-react"
import { useState } from "react"
import { type WindowInfo, WindowManager, type WindowTree } from "@/components/ui/window-manager"
import { Section, Specimen } from "./Specimen"

const START: WindowTree = {
  root: {
    type: "row",
    children: [
      { type: "stack", id: "left", weight: 60, wins: ["draft", "notes"], selected: 0 },
      { type: "stack", id: "right", weight: 40, wins: ["browser"] },
    ],
  },
  floats: [
    {
      id: "f",
      rect: { x: 40, y: 150, w: 220, h: 110 },
      root: { type: "row", children: [{ type: "stack", wins: ["scratch"] }] },
    },
  ],
}

const WINDOWS: WindowInfo[] = [
  { id: "draft", title: "Launch notes", icon: <FileText className="size-3.5" />, dirty: true },
  { id: "notes", title: "Notes", icon: <StickyNote className="size-3.5" /> },
  { id: "browser", title: "Example Domain", icon: <Globe className="size-3.5" /> },
  { id: "scratch", title: "Scratch", icon: <StickyNote className="size-3.5" /> },
]

export function Windows() {
  const [tree, setTree] = useState<WindowTree>(START)
  const [focus, setFocus] = useState<string | undefined>("draft")
  return (
    <Section
      title="window-manager"
      about="Drag a tab to move, split, tab or float a window. The tree is local to this window."
    >
      <Specimen label="two stacks and a float" wide>
        <div
          className="relative h-80 w-full overflow-hidden rounded-md border border-border bg-muted/40"
          style={{ "--window-gap": "6px" } as React.CSSProperties}
        >
          <WindowManager
            tree={tree}
            windows={WINDOWS}
            focus={focus}
            onArrange={(t, f) => {
              setTree(t)
              setFocus(f)
            }}
            onClose={() => {}}
          >
            {(id) => (
              <div className="p-3 text-xs text-muted-foreground">
                The body of “{WINDOWS.find((w) => w.id === id)?.title}”.
              </div>
            )}
          </WindowManager>
        </div>
      </Specimen>
    </Section>
  )
}
