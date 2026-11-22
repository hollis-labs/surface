/**
 * Inbox's window: the owner's Tether mail. The list on the left (unread, all, archived), the
 * message on the right, read in full. Reading marks nothing by itself; Mark read, Archive and
 * Reply are the owner's. Mail bodies are their senders' text, shown as Markdown.
 */
import { Icon, type ModuleWeb, useEvent, type ViewProps } from "@hollis-labs/surface-page/sdk"
import { useCallback, useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Item, ItemContent, ItemDescription, ItemTitle } from "@/components/ui/item"
import { Markdown } from "@/components/ui/markdown"
import { Textarea } from "@/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

interface Message {
  id: string
  account: string
  from: string
  from_name?: string
  to: string
  kind: string
  thread?: string
  subject: string
  body: string
  created: string
  read: boolean
  archived: boolean
}
type Box = "unread" | "all" | "archived"

const short = (urn: string) => urn.replace(/^msg:\/\/[^/]+\/[^/]+\//, "")
const when = (iso: string) => {
  const d = new Date(iso)
  return Number.isNaN(d.getTime())
    ? ""
    : d.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })
}

function Inbox({ win, desk, api }: ViewProps) {
  const [box, setBox] = useState<Box>("unread")
  const [list, setList] = useState<Message[] | null>(null)
  const [open, setOpen] = useState<Message | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState("")
  const [replying, setReplying] = useState(false)
  const [composing, setComposing] = useState(false)
  const [text, setText] = useState("")
  const [to, setTo] = useState("")
  const [subject, setSubject] = useState("")

  const load = useCallback(() => {
    api
      .get<Message[]>(`messages?box=${box}`)
      .then((l) => {
        setList(l ?? [])
        setError("")
      })
      .catch((e: Error) => setError(e.message))
  }, [api, box])
  useEffect(load, [load])
  useEvent("inbox.changed", load)
  useEffect(() => desk.report(win.id, { kind: "data", ref: "inbox" }), [desk, win.id])

  const show = (m: Message) => {
    setReplying(false)
    setComposing(false)
    api
      .get<Message>(`messages/${encodeURIComponent(m.id)}?as=${encodeURIComponent(m.account)}`)
      .then(setOpen)
      .catch((e: Error) => setError(e.message))
  }
  const act = (label: string, p: Promise<unknown>, after?: () => void) => {
    setBusy(label)
    p.then(() => {
      after?.()
      load()
    })
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(""))
  }
  const as = open ? `as=${encodeURIComponent(open.account)}` : ""

  return (
    <div className="@container flex h-full min-h-0 flex-col bg-background">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-2">
        <span className="text-sm font-semibold">Inbox</span>
        <ToggleGroup size="sm" spacing={0} value={[box]} onValueChange={(v: string[]) => v[0] && setBox(v[0] as Box)}>
          <ToggleGroupItem value="unread">Unread</ToggleGroupItem>
          <ToggleGroupItem value="all">All</ToggleGroupItem>
          <ToggleGroupItem value="archived">Archived</ToggleGroupItem>
        </ToggleGroup>
        <div className="ml-auto flex items-center gap-1">
          <Button size="sm" variant="ghost" aria-label="Check for mail" title="Check for mail" onClick={load}>
            <Icon name="refresh-cw" />
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setComposing(true)
              setReplying(false)
              setOpen(null)
            }}
          >
            <Icon name="pen-line" />
            New message
          </Button>
        </div>
      </div>
      {error && <p className="border-b px-3 py-1 text-xs text-destructive">{error}</p>}
      <div className="flex min-h-0 flex-1">
        <div className="flex w-[22rem] shrink-0 flex-col gap-0.5 overflow-auto border-r p-1.5 @max-3xl:w-56">
          {list === null && <p className="p-3 text-xs text-muted-foreground">Reading your mail…</p>}
          {list?.length === 0 && (
            <Empty>
              <EmptyHeader>
                <EmptyTitle>{box === "unread" ? "Nothing unread" : "No mail here"}</EmptyTitle>
                <EmptyDescription>{box === "unread" ? "New mail shows here as it arrives." : ""}</EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
          {list?.map((m) => (
            <Item
              key={`${m.account}-${m.id}`}
              size="sm"
              variant={open?.id === m.id ? "muted" : "default"}
              render={<a href={`#${m.id}`} />}
              onClick={(e) => {
                e.preventDefault()
                show(m)
              }}
            >
              <ItemContent>
                <ItemTitle className="w-full">
                  {!m.read && box !== "archived" && <span className="size-1.5 shrink-0 rounded-full bg-primary" />}
                  <span className="min-w-0 flex-1 truncate">{m.from_name || short(m.from)}</span>
                  <span className="shrink-0 font-normal text-muted-foreground text-xs">{when(m.created)}</span>
                </ItemTitle>
                <ItemDescription className="truncate">{m.subject || m.body.slice(0, 80)}</ItemDescription>
              </ItemContent>
            </Item>
          ))}
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
          {composing ? (
            <div className="flex flex-col gap-2 p-3">
              <span className="text-sm font-semibold">New message</span>
              <Input placeholder="To (msg://…)" value={to} onChange={(e) => setTo(e.target.value)} />
              <Input placeholder="Subject" value={subject} onChange={(e) => setSubject(e.target.value)} />
              <Textarea
                className="min-h-48"
                placeholder="Write it here"
                value={text}
                onChange={(e) => setText(e.target.value)}
              />
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="ghost" onClick={() => setComposing(false)}>
                  Cancel
                </Button>
                <Button
                  size="sm"
                  disabled={busy !== "" || !to.startsWith("msg://") || !text.trim()}
                  onClick={() =>
                    act("send", api.post("send", { to, subject, body: text }), () => {
                      setComposing(false)
                      setText("")
                      setSubject("")
                    })
                  }
                >
                  Send
                </Button>
              </div>
            </div>
          ) : open ? (
            <>
              <div className="flex shrink-0 flex-wrap items-start gap-2 border-b px-4 py-3">
                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="text-sm font-semibold">{open.subject || "(no subject)"}</span>
                  <span className="text-xs text-muted-foreground">
                    {open.from_name ? `${open.from_name} · ` : ""}
                    <span className="font-mono">{short(open.from)}</span> → {short(open.account)} · {when(open.created)}
                  </span>
                  <span className="flex flex-wrap gap-1">
                    <Badge variant="outline">{open.kind}</Badge>
                    {open.thread && <Badge variant="outline">{open.thread}</Badge>}
                  </span>
                </div>
                <div className="flex flex-wrap items-center gap-1">
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={busy !== ""}
                    onClick={() => act("read", api.post(`messages/${encodeURIComponent(open.id)}/read?${as}`, {}))}
                  >
                    <Icon name="mail-open" />
                    Mark read
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={busy !== ""}
                    onClick={() =>
                      act(
                        "archive",
                        api.post(
                          `messages/${encodeURIComponent(open.id)}/archive?${as}${box === "archived" ? "&undo=1" : ""}`,
                          {},
                        ),
                        () => setOpen(null),
                      )
                    }
                  >
                    <Icon name={box === "archived" ? "archive-arrow-up" : "archive"} />
                    {box === "archived" ? "Unarchive" : "Archive"}
                  </Button>
                  <Button size="sm" onClick={() => setReplying(true)}>
                    <Icon name="reply" />
                    Reply
                  </Button>
                </div>
              </div>
              <div className="min-h-0 flex-1 overflow-auto px-4 py-3">
                <Markdown className="text-sm">{open.body}</Markdown>
              </div>
              {replying && (
                <div className="flex shrink-0 flex-col gap-2 border-t p-3">
                  <Textarea
                    autoFocus
                    className="min-h-28"
                    placeholder={`Reply to ${open.from_name || short(open.from)}`}
                    value={text}
                    onChange={(e) => setText(e.target.value)}
                  />
                  <div className="flex justify-end gap-2">
                    <Button size="sm" variant="ghost" onClick={() => setReplying(false)}>
                      Cancel
                    </Button>
                    <Button
                      size="sm"
                      disabled={busy !== "" || !text.trim()}
                      onClick={() =>
                        act(
                          "reply",
                          api.post(`messages/${encodeURIComponent(open.id)}/reply?${as}`, { body: text }),
                          () => {
                            setReplying(false)
                            setText("")
                          },
                        )
                      }
                    >
                      Send reply
                    </Button>
                  </div>
                </div>
              )}
            </>
          ) : (
            <Empty className="h-full">
              <EmptyHeader>
                <EmptyTitle>No message open</EmptyTitle>
                <EmptyDescription>Choose one on the left.</EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
        </div>
      </div>
    </div>
  )
}

export default { views: { "inbox/main": Inbox } } satisfies ModuleWeb
