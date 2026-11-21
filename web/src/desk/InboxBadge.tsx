/** The Inbox owns refreshes; the bar hydrates once and follows its unread events. */
import { useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Icon } from "../sdk"
import { onConnection, onEvent } from "../sdk/runtime"
import { moduleApi } from "./moduleApi"
import { desk } from "./store"

type Unread = { unread: number; ready: boolean }

export function InboxBadge() {
  const [unread, setUnread] = useState<number | null>(null)
  useEffect(() => {
    let live = true
    let version = 0
    const apply = (data: Unread) => {
      if (live && data.ready && Number.isInteger(data.unread) && data.unread >= 0) setUnread(data.unread)
    }
    const load = async () => {
      const request = ++version
      try {
        const data = await moduleApi("inbox").get<Unread>("unread")
        if (request === version) apply(data)
      } catch {
        /* Keep the last count while the connection recovers. */
      }
    }
    const off = onEvent("inbox.unread", (ev) => {
      version++ // An event supersedes any initial/reconnect request still in flight.
      apply(ev.data as Unread)
    })
    const disconnected = onConnection((up) => {
      if (up) void load()
    })
    void load()
    return () => {
      live = false
      off()
      disconnected()
    }
  }, [])
  const label = unread === null ? "Inbox" : `Inbox: ${unread} unread`
  return (
    <Button variant="ghost" size="sm" title={label} aria-label={label} onClick={() => void desk.open("inbox")}>
      <Icon name="inbox" />
      {unread !== null && unread > 0 && <Badge variant="secondary">{unread > 99 ? "99+" : unread}</Badge>}
    </Button>
  )
}
