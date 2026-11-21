/**
 * Reusable `<ObjectActionsMenu />` component for any referenceable object in Surface.
 * Renders a compact trigger button that opens a grouped dropdown menu with built-in and
 * module-registered object actions (Copy, Add to Context, Pin, Bookmark, Mark, Forward,
 * Send to Location, Download, plus custom hooks).
 */
import { type ComponentProps, useState } from "react"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Icon } from "../sdk/icon"
import {
  createObjectActionContext,
  formatObjectRef,
  type ObjectActionDef,
  type ObjectActionGroup,
  type ObjectRef,
  useObjectActions,
} from "../sdk/objectActions"

const GROUP_ORDER: ObjectActionGroup[] = ["copy", "context", "organize", "send", "download", "custom"]

export interface ObjectActionsMenuProps {
  obj?: ObjectRef
  object?: ObjectRef
  /** Optional trigger label (defaults to icon-only ellipsis button). */
  label?: string
  /** Optional trigger icon name (defaults to "ellipsis"). */
  icon?: string
  size?: ComponentProps<typeof Button>["size"]
  variant?: ComponentProps<typeof Button>["variant"]
  className?: string
  align?: "start" | "center" | "end"
  /** Called right before an action runs (e.g. to close a parent hover card). */
  onAction?: (actionId: string) => void
}

export function ObjectActionsMenu({
  obj: objProp,
  object: objectProp,
  label,
  icon = "ellipsis",
  size = "icon-xs",
  variant = "ghost",
  className,
  align = "end",
  onAction,
}: ObjectActionsMenuProps) {
  const obj = objProp ?? objectProp ?? { kind: "object", id: "" }
  const actions = useObjectActions(obj)
  const [toast, setToast] = useState("")
  const ctx = createObjectActionContext((msg) => {
    setToast(msg)
    window.setTimeout(() => setToast(""), 1800)
  })

  const byGroup = new Map<ObjectActionGroup, ObjectActionDef[]>()
  for (const a of actions) {
    const g = a.group ?? "custom"
    byGroup.set(g, [...(byGroup.get(g) ?? []), a])
  }
  const activeGroups = GROUP_ORDER.filter((g) => (byGroup.get(g)?.length ?? 0) > 0)
  const refText = formatObjectRef(obj)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant={variant}
            size={label ? "xs" : size}
            className={className}
            title={toast || `Actions for ${obj.title || refText}`}
            aria-label={`Actions for ${obj.title || refText}`}
            onClick={(e) => e.stopPropagation()}
            onPointerDown={(e) => e.stopPropagation()}
          >
            <Icon name={icon} className="size-3.5" />
            {label && <span>{label}</span>}
          </Button>
        }
      />
      <DropdownMenuContent align={align} className="min-w-52" onClick={(e) => e.stopPropagation()}>
        <DropdownMenuGroup>
          <DropdownMenuLabel className="flex items-center gap-1.5 truncate font-mono text-[10.5px] text-muted-foreground">
            <span className="truncate">{obj.title || refText}</span>
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        {activeGroups.map((groupKey, idx) => {
          const list = byGroup.get(groupKey) ?? []
          return (
            <div key={groupKey}>
              {idx > 0 && <DropdownMenuSeparator />}
              <DropdownMenuGroup>
                {list.map((action) => {
                  const children = action.children?.filter((c) => (c.when ? c.when(obj) : true))
                  if (children && children.length > 0) {
                    return (
                      <DropdownMenuSub key={action.id}>
                        <DropdownMenuSubTrigger>
                          {action.icon && <Icon name={action.icon} className="size-3.5 text-muted-foreground" />}
                          <span>{action.label}</span>
                        </DropdownMenuSubTrigger>
                        <DropdownMenuSubContent className="min-w-48">
                          {children.map((child) => (
                            <DropdownMenuItem
                              key={child.id}
                              onClick={() => {
                                onAction?.(child.id)
                                void child.run(obj, ctx)
                              }}
                            >
                              {child.icon && <Icon name={child.icon} className="size-3.5 text-muted-foreground" />}
                              <span>{child.label}</span>
                            </DropdownMenuItem>
                          ))}
                        </DropdownMenuSubContent>
                      </DropdownMenuSub>
                    )
                  }
                  return (
                    <DropdownMenuItem
                      key={action.id}
                      onClick={() => {
                        onAction?.(action.id)
                        void action.run?.(obj, ctx)
                      }}
                    >
                      {action.icon && <Icon name={action.icon} className="size-3.5 text-muted-foreground" />}
                      <span>{action.label}</span>
                      {action.shortcut && <DropdownMenuShortcut>{action.shortcut}</DropdownMenuShortcut>}
                    </DropdownMenuItem>
                  )
                })}
              </DropdownMenuGroup>
            </div>
          )
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
