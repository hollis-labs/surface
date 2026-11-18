/** Showing things: rows, tables, cards, regions, status. */
import { ChevronRight, FileText, Folder, Inbox } from "lucide-react"
import { Avatar, AvatarFallback, AvatarGroup, AvatarGroupCount } from "@/components/ui/avatar"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Item, ItemActions, ItemContent, ItemDescription, ItemMedia, ItemTitle } from "@/components/ui/item"
import { Progress } from "@/components/ui/progress"
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Table, TableBody, TableCaption, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Section, Specimen } from "./Specimen"

const ROWS = [
  { name: "notes", kind: "module", state: "running" },
  { name: "drafts", kind: "module", state: "running" },
  { name: "builder", kind: "module", state: "stopped" },
]

export function Display() {
  return (
    <>
      <Section title="Rows" about="Item in each variant and size; a Table.">
        {(["default", "outline", "muted"] as const).map((v) => (
          <Specimen key={v} label={`Item variant="${v}"`}>
            <Item variant={v}>
              <ItemMedia variant="icon">
                <FileText />
              </ItemMedia>
              <ItemContent>
                <ItemTitle>Launch notes</ItemTitle>
                <ItemDescription>markdown · r2 · 7 lines</ItemDescription>
              </ItemContent>
              <ItemActions>
                <Button variant="ghost" size="icon-sm" aria-label="Open">
                  <ChevronRight />
                </Button>
              </ItemActions>
            </Item>
          </Specimen>
        ))}
        <Specimen label='Item size="sm" and "xs"'>
          <div className="flex w-full flex-col gap-1">
            <Item size="sm" variant="outline">
              <ItemContent>
                <ItemTitle>Small</ItemTitle>
              </ItemContent>
            </Item>
            <Item size="xs" variant="outline">
              <ItemContent>
                <ItemTitle>Extra small</ItemTitle>
              </ItemContent>
            </Item>
          </div>
        </Specimen>
        <Specimen label="Table" wide>
          <Table>
            <TableCaption>The modules of this app.</TableCaption>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Kind</TableHead>
                <TableHead className="text-right">State</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {ROWS.map((r) => (
                <TableRow key={r.name}>
                  <TableCell className="font-medium">{r.name}</TableCell>
                  <TableCell>{r.kind}</TableCell>
                  <TableCell className="text-right">{r.state}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Specimen>
      </Section>
      <Section title="Regions">
        <Specimen label="Card: default and sm">
          <Card className="w-full">
            <CardHeader>
              <CardTitle>A card</CardTitle>
              <CardDescription>With a header, content and a footer.</CardDescription>
              <CardAction>
                <Button variant="outline" size="xs">
                  Action
                </Button>
              </CardAction>
            </CardHeader>
            <CardContent className="text-sm">The body of the card.</CardContent>
            <CardFooter>
              <Button size="sm">Save</Button>
            </CardFooter>
          </Card>
          <Card size="sm" className="w-full">
            <CardHeader>
              <CardTitle>Small</CardTitle>
              <CardDescription>size="sm"</CardDescription>
            </CardHeader>
          </Card>
        </Specimen>
        <Specimen label="Empty">
          <Empty className="border border-dashed border-border">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Inbox />
              </EmptyMedia>
              <EmptyTitle>Nothing here yet</EmptyTitle>
              <EmptyDescription>Things you add show up here.</EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button size="sm">Add one</Button>
            </EmptyContent>
          </Empty>
        </Specimen>
        <Specimen label="ScrollArea">
          <ScrollArea className="h-28 w-full rounded-md border border-border">
            <div className="flex flex-col gap-1 p-2 text-sm">
              {Array.from({ length: 12 }, (_, i) => `Line ${i + 1}`).map((l) => (
                <span key={l}>{l}</span>
              ))}
            </div>
          </ScrollArea>
        </Specimen>
        <Specimen label="ResizablePanelGroup">
          <div className="h-28 w-full rounded-md border border-border">
            <ResizablePanelGroup orientation="horizontal">
              <ResizablePanel defaultSize="40" className="grid place-items-center text-sm">
                One
              </ResizablePanel>
              <ResizableHandle withHandle />
              <ResizablePanel className="grid place-items-center text-sm">Two</ResizablePanel>
            </ResizablePanelGroup>
          </div>
        </Specimen>
        <Specimen label="Collapsible (open)">
          <Collapsible defaultOpen className="w-full">
            <CollapsibleTrigger render={<Button variant="ghost" size="sm" />}>
              <Folder /> Details
            </CollapsibleTrigger>
            <CollapsibleContent className="px-2 pt-1 text-sm text-muted-foreground">
              What was folded away.
            </CollapsibleContent>
          </Collapsible>
        </Specimen>
        <Specimen label="Breadcrumb and Separator">
          <Breadcrumb>
            <BreadcrumbList>
              <BreadcrumbItem>
                <BreadcrumbLink href="#gallery">Home</BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbLink href="#gallery">Notes</BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                <BreadcrumbPage>Launch</BreadcrumbPage>
              </BreadcrumbItem>
            </BreadcrumbList>
          </Breadcrumb>
          <Separator />
        </Specimen>
      </Section>
      <Section title="Status">
        <Specimen label="Spinner, Skeleton, Progress">
          <Spinner />
          <div className="flex w-40 flex-col gap-1.5">
            <Skeleton className="h-3 w-full" />
            <Skeleton className="h-3 w-2/3" />
          </div>
          <Progress value={62} className="w-40" aria-label="62 percent" />
        </Specimen>
        <Specimen label="Avatar: sizes and a group">
          <Avatar size="sm">
            <AvatarFallback>AL</AvatarFallback>
          </Avatar>
          <Avatar>
            <AvatarFallback>AL</AvatarFallback>
          </Avatar>
          <Avatar size="lg">
            <AvatarFallback>AL</AvatarFallback>
          </Avatar>
          <AvatarGroup>
            <Avatar>
              <AvatarFallback>A</AvatarFallback>
            </Avatar>
            <Avatar>
              <AvatarFallback>B</AvatarFallback>
            </Avatar>
            <AvatarGroupCount>+3</AvatarGroupCount>
          </AvatarGroup>
        </Specimen>
      </Section>
    </>
  )
}
