/** What opens over the page, each from its trigger; Command and a menu also inline. */
import { Copy, Settings, Trash2 } from "lucide-react"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@/components/ui/command"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuShortcut,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { Section, Specimen } from "./Specimen"

export function Overlays() {
  return (
    <Section title="Over the page" about="Open each from its trigger. Command is shown inline as well.">
      <Specimen label="Dialog and AlertDialog">
        <Dialog>
          <DialogTrigger render={<Button variant="outline">Open dialog</Button>} />
          <DialogContent>
            <DialogHeader>
              <DialogTitle>A dialog</DialogTitle>
              <DialogDescription>Something to read or fill in before going on.</DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <DialogClose render={<Button variant="outline">Cancel</Button>} />
              <Button>Save</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
        <AlertDialog>
          <AlertDialogTrigger render={<Button variant="destructive">Delete…</Button>} />
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Delete the draft?</AlertDialogTitle>
              <AlertDialogDescription>Its revisions go too. This cannot be undone.</AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction>Delete</AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </Specimen>
      <Specimen label="Sheet, Popover, Tooltip">
        <Sheet>
          <SheetTrigger render={<Button variant="outline">Open sheet</Button>} />
          <SheetContent>
            <SheetHeader>
              <SheetTitle>A sheet</SheetTitle>
              <SheetDescription>A panel from the edge of the screen.</SheetDescription>
            </SheetHeader>
            <SheetFooter>
              <Button>Done</Button>
            </SheetFooter>
          </SheetContent>
        </Sheet>
        <Popover>
          <PopoverTrigger render={<Button variant="outline">Open popover</Button>} />
          <PopoverContent>
            <PopoverHeader>
              <PopoverTitle>A popover</PopoverTitle>
              <PopoverDescription>A little more, next to what it is about.</PopoverDescription>
            </PopoverHeader>
          </PopoverContent>
        </Popover>
        <Tooltip>
          <TooltipTrigger render={<Button variant="ghost" size="icon" aria-label="Settings" />}>
            <Settings />
          </TooltipTrigger>
          <TooltipContent>Settings</TooltipContent>
        </Tooltip>
      </Specimen>
      <Specimen label="DropdownMenu and ContextMenu">
        <DropdownMenu>
          <DropdownMenuTrigger render={<Button variant="outline">Open menu</Button>} />
          <DropdownMenuContent>
            <DropdownMenuLabel>Draft</DropdownMenuLabel>
            <DropdownMenuItem>
              <Copy /> Duplicate <DropdownMenuShortcut>⌘D</DropdownMenuShortcut>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive">
              <Trash2 /> Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <ContextMenu>
          <ContextMenuTrigger className="grid h-16 w-48 place-items-center rounded-md border border-dashed border-border text-xs text-muted-foreground">
            Right-click here
          </ContextMenuTrigger>
          <ContextMenuContent>
            <ContextMenuItem>
              Copy <ContextMenuShortcut>⌘C</ContextMenuShortcut>
            </ContextMenuItem>
            <ContextMenuItem>Rename</ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      </Specimen>
      <Specimen label="Command (inline)">
        <Command className="w-full rounded-lg border border-border">
          <CommandInput placeholder="Run a command…" />
          <CommandList>
            <CommandEmpty>Nothing matches.</CommandEmpty>
            <CommandGroup heading="Desk">
              <CommandItem>
                Open Notes <CommandShortcut>⌘N</CommandShortcut>
              </CommandItem>
              <CommandItem>Tile every window</CommandItem>
              <CommandItem>Show the agent</CommandItem>
            </CommandGroup>
          </CommandList>
        </Command>
      </Specimen>
    </Section>
  )
}
