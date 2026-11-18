/** Actions and inputs: every variant and size, and their states. */
import { Bold, Italic, Mail, Plus, Search, Underline } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from "@/components/ui/input-group"
import { Kbd, KbdGroup } from "@/components/ui/kbd"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { TextEditor } from "@/components/ui/text-editor"
import { Textarea } from "@/components/ui/textarea"
import { Toggle } from "@/components/ui/toggle"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Section, Specimen } from "./Specimen"

const VARIANTS = ["default", "outline", "secondary", "ghost", "destructive", "link"] as const
const SIZES = ["xs", "sm", "default", "lg"] as const
const ICON_SIZES = ["icon-xs", "icon-sm", "icon", "icon-lg"] as const
const FRUIT = [
  { value: "apple", label: "Apple" },
  { value: "pear", label: "Pear" },
  { value: "plum", label: "Plum" },
]

export function Controls() {
  return (
    <>
      <Section title="Button" about="variant × size; icon sizes; disabled.">
        {VARIANTS.map((v) => (
          <Specimen key={v} label={`variant="${v}"`}>
            {SIZES.map((s) => (
              <Button key={s} variant={v} size={s}>
                {s}
              </Button>
            ))}
            <Button variant={v} disabled>
              disabled
            </Button>
          </Specimen>
        ))}
        <Specimen label="icon sizes, with an icon and text">
          {ICON_SIZES.map((s) => (
            <Button key={s} variant="outline" size={s} aria-label={s}>
              <Plus />
            </Button>
          ))}
          <Button>
            <Mail /> With icon
          </Button>
        </Specimen>
      </Section>
      <Section title="Badge and Kbd">
        <Specimen label="Badge variants">
          {(["default", "secondary", "outline", "destructive", "ghost", "link"] as const).map((v) => (
            <Badge key={v} variant={v}>
              {v}
            </Badge>
          ))}
        </Specimen>
        <Specimen label="Kbd">
          <Kbd>⌘K</Kbd>
          <KbdGroup>
            <Kbd>Shift</Kbd>
            <Kbd>Enter</Kbd>
          </KbdGroup>
        </Specimen>
      </Section>
      <Section title="Text entry">
        <Specimen label="Input: default, disabled, invalid">
          <Input placeholder="Placeholder" aria-label="default" />
          <Input placeholder="Disabled" disabled aria-label="disabled" />
          <Input defaultValue="Not an email" aria-invalid aria-label="invalid" />
        </Specimen>
        <Specimen label="InputGroup">
          <InputGroup>
            <InputGroupAddon>
              <Search />
            </InputGroupAddon>
            <InputGroupInput placeholder="Search" aria-label="Search" />
            <InputGroupAddon align="inline-end">
              <InputGroupButton>Go</InputGroupButton>
            </InputGroupAddon>
          </InputGroup>
        </Specimen>
        <Specimen label="Textarea">
          <Textarea placeholder="Write something" aria-label="Textarea" />
        </Specimen>
        <Specimen label="TextEditor (a document's source)">
          <div className="h-24 w-full rounded-md border border-border">
            <TextEditor aria-label="Source" defaultValue={"# A heading\n\nSome **Markdown**, in the monospace face."} />
          </div>
        </Specimen>
        <Specimen label="Field: label, description, error" wide>
          <Field>
            <FieldLabel htmlFor="g-name">Name</FieldLabel>
            <Input id="g-name" placeholder="Ada" />
            <FieldDescription>What the agent calls you.</FieldDescription>
          </Field>
          <Field data-invalid="true">
            <FieldLabel htmlFor="g-mail">Email</FieldLabel>
            <Input id="g-mail" defaultValue="ada@" aria-invalid />
            <FieldError>That is not a whole address.</FieldError>
          </Field>
        </Specimen>
      </Section>
      <Section title="Choices">
        <Specimen label="Select: default, sm, disabled">
          <Select defaultValue="pear" items={FRUIT}>
            <SelectTrigger aria-label="Fruit">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {FRUIT.map((f) => (
                <SelectItem key={f.value} value={f.value}>
                  {f.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select defaultValue="plum" items={FRUIT}>
            <SelectTrigger size="sm" aria-label="Fruit, small">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {FRUIT.map((f) => (
                <SelectItem key={f.value} value={f.value}>
                  {f.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select disabled items={FRUIT}>
            <SelectTrigger aria-label="Fruit, disabled">
              <SelectValue placeholder="Disabled" />
            </SelectTrigger>
            <SelectContent />
          </Select>
        </Specimen>
        <Specimen label="Checkbox and Switch: off, on, disabled">
          <Label className="gap-2">
            <Checkbox /> off
          </Label>
          <Label className="gap-2">
            <Checkbox defaultChecked /> on
          </Label>
          <Label className="gap-2">
            <Checkbox disabled /> disabled
          </Label>
          <Switch aria-label="off" />
          <Switch defaultChecked aria-label="on" />
          <Switch disabled aria-label="disabled" />
        </Specimen>
        <Specimen label="Toggle and ToggleGroup (default, outline, sm)">
          <Toggle aria-label="Bold" defaultPressed>
            <Bold />
          </Toggle>
          <ToggleGroup defaultValue={["bold"]} aria-label="Style">
            <ToggleGroupItem value="bold" aria-label="Bold">
              <Bold />
            </ToggleGroupItem>
            <ToggleGroupItem value="italic" aria-label="Italic">
              <Italic />
            </ToggleGroupItem>
            <ToggleGroupItem value="underline" aria-label="Underline">
              <Underline />
            </ToggleGroupItem>
          </ToggleGroup>
          <ToggleGroup variant="outline" size="sm" spacing={0} defaultValue={["review"]} aria-label="Mode">
            <ToggleGroupItem value="review">Review</ToggleGroupItem>
            <ToggleGroupItem value="edit">Edit</ToggleGroupItem>
            <ToggleGroupItem value="compare">Compare</ToggleGroupItem>
          </ToggleGroup>
        </Specimen>
        <Specimen label="Tabs: default and line">
          <Tabs defaultValue="one">
            <TabsList>
              <TabsTrigger value="one">One</TabsTrigger>
              <TabsTrigger value="two">Two</TabsTrigger>
              <TabsTrigger value="three" disabled>
                Three
              </TabsTrigger>
            </TabsList>
            <TabsContent value="one" className="pt-2 text-sm text-muted-foreground">
              The first tab's content.
            </TabsContent>
          </Tabs>
          <Tabs defaultValue="two">
            <TabsList variant="line">
              <TabsTrigger value="one">One</TabsTrigger>
              <TabsTrigger value="two">Two</TabsTrigger>
            </TabsList>
          </Tabs>
        </Specimen>
      </Section>
    </>
  )
}
