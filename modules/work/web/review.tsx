/**
 * An item's review, in its detail: who rounds go to, the draft round (the owner's notes and
 * the agent's drafts, not yet sent), Send round, the rounds sent with the team's answers,
 * and sign-off. Sending and signing off are only ever the owner's click here; the agent can
 * draft notes and nothing more.
 */
import { type Desk, Icon } from "@hollis-labs/surface-page/sdk"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Input } from "@/components/ui/input"
import { Markdown } from "@/components/ui/markdown"
import { Textarea } from "@/components/ui/textarea"
import type { WorkClient, WorkDetail, WorkRound } from "./api"
import { Pick, Sec } from "./parts"
import { ago, isConflict, message } from "./util"

const STATE_LABEL: Record<string, string> = { open: "Open", done: "Done", declined: "Declined" }

interface Draft {
  kind: "comment" | "suggestion"
  section: string
  quote: string
  replace: string
  text: string
}

function NoteBody({ quote, replace, kind, text }: { quote?: string; replace?: string; kind: string; text?: string }) {
  return (
    <>
      {quote && <div className="work-text text-destructive text-xs">− {quote}</div>}
      {kind === "suggestion" && <div className="work-text text-success text-xs">+ {replace}</div>}
      {text && <Markdown className="wrap-anywhere">{text}</Markdown>}
    </>
  )
}

export function Review({
  w,
  desk,
  ws,
  d,
  reload,
  changed,
}: {
  w: WorkClient
  desk: Desk
  ws: string
  d: WorkDetail
  reload: () => Promise<void>
  changed: () => void
}) {
  const [status, setStatus] = useState("")
  const [busy, setBusy] = useState(false)
  const [adding, setAdding] = useState<Draft | null>(null)
  const [owners, setOwners] = useState<string | null>(null)

  const act = async (f: () => Promise<string | undefined>) => {
    setBusy(true)
    try {
      setStatus((await f()) ?? "")
    } catch (e) {
      setStatus(message(e))
    }
    setBusy(false)
    await reload()
    changed()
  }

  /** Start a note; a selection in one of the item's sections becomes a suggestion's quote. */
  const start = (kind: "comment" | "suggestion") => {
    let section = ""
    let quote = ""
    const sel = window.getSelection()
    const text = sel?.toString() ?? ""
    const node = sel?.anchorNode
    const el = node instanceof Element ? node : node?.parentElement
    const sec = el?.closest("[data-section]")?.getAttribute("data-section")
    if (sec && text.trim() && (d.sections[sec] ?? "").includes(text)) {
      section = sec
      quote = text
    }
    setAdding({ kind, section, quote, replace: quote, text: "" })
  }

  const send = async () => {
    const to = d.owners.length ? d.owners.join(", ") : "nobody (no owners are set)"
    const yes = await desk.confirm({
      title: `Send round ${(d.rounds[0]?.round ?? 0) + 1} on “${d.title}”?`,
      message: `${d.notes.length} note${d.notes.length === 1 ? "" : "s"} go to Caster as a round on this item, and mail goes from you to ${to}.${d.task ? ` Its Torque task (${d.task}) gets a comment.` : ""}`,
      confirmLabel: "Send round",
    })
    if (!yes) return
    await act(async () => {
      const r = await w.sendRound(ws, d.id)
      return `Round ${r.round} sent (${r.notes} note${r.notes === 1 ? "" : "s"}).${r.mailed.length ? ` Mailed ${r.mailed.join(", ")}.` : ""}${r.not_mailed.length ? ` Not mailed: ${r.not_mailed.join(", ")}.` : ""}`
    })
  }

  const signOff = async () => {
    const yes = await desk.confirm({
      title: `Sign off “${d.title}”?`,
      message: `Revision ${d.rev}'s text is approved as it is: the sign-off is kept on the item and that revision pinned in CAS.${d.stage === "review" ? " It moves on from review." : ""}${d.owners.length ? ` ${d.owners.join(", ")} ${d.owners.length === 1 ? "is" : "are"} told.` : ""}${d.notes.length ? " Your draft notes stay unsent." : ""}`,
      confirmLabel: "Sign off",
    })
    if (!yes) return
    const done = (r: Awaited<ReturnType<WorkClient["signOff"]>>) =>
      `Signed off at revision ${r.rev}.${r.stage ? ` Now ${r.stage}.` : ""}${r.mailed.length ? ` Told ${r.mailed.join(", ")}.` : ""}${r.problem ? ` ${r.problem}` : ""}`
    await act(async () => {
      try {
        return done(await w.signOff(ws, d.id))
      } catch (e) {
        if (!isConflict(e)) throw e
        const anyway = await desk.confirm({
          title: "Some notes are still open",
          message: `${message(e)}. Sign off anyway?`,
          confirmLabel: "Sign off anyway",
        })
        if (!anyway) return ""
        return done(await w.signOff(ws, d.id, true))
      }
    })
  }

  const saveNote = (a: Draft) =>
    void act(async () => {
      await w.addNote(ws, d.id, {
        kind: a.kind,
        section: a.section || undefined,
        quote: a.kind === "suggestion" ? a.quote : undefined,
        replace: a.kind === "suggestion" ? a.replace : undefined,
        text: a.text.trim() || undefined,
      })
      setAdding(null)
      return ""
    })

  const so = d.sign_off
  return (
    <Sec title="Review">
      {so && (
        <div className={`flex items-center gap-1.5 text-sm ${d.stale_sign ? "text-warning" : "text-success"}`}>
          <Icon name="badge-check" size={14} />
          <span>
            Signed off {ago(so.at)} at revision {so.rev}
            {d.stale_sign ? "; the text has changed since" : ""}
          </span>
          <Button
            variant="link"
            size="xs"
            className="ml-auto"
            disabled={busy}
            onClick={() =>
              void act(async () => {
                await w.unsign(ws, d.id)
                return "The sign-off is taken off."
              })
            }
          >
            Take off
          </Button>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-1.5 text-sm">
        <span className="text-muted-foreground text-xs">Rounds go to</span>
        {owners === null ? (
          <>
            <span title="The workspace's owners: Tether addresses">
              {d.owners.length ? d.owners.join(", ") : "nobody yet"}
            </span>
            <Button variant="link" size="xs" onClick={() => setOwners(d.owners.join(", "))}>
              Change
            </Button>
          </>
        ) : (
          <div className="flex w-full items-center gap-1.5">
            <Input
              autoFocus
              className="h-7 flex-1"
              value={owners}
              onChange={(e) => setOwners(e.target.value)}
              placeholder="msg://agent/…, msg://agent/…"
              aria-label="Owners"
            />
            <Button
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => {
                const list = owners
                  .split(/[,\s]+/)
                  .map((x) => x.trim())
                  .filter(Boolean)
                void act(async () => {
                  await w.setOwners(ws, list)
                  setOwners(null)
                  return "Owners saved for this workspace."
                })
              }}
            >
              Save
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setOwners(null)}>
              Cancel
            </Button>
          </div>
        )}
      </div>

      <div className="flex items-center gap-1.5">
        <span className="text-muted-foreground text-xs">
          {d.notes.length
            ? `Draft round: ${d.notes.length} note${d.notes.length === 1 ? "" : "s"}, not sent`
            : "No notes in the draft round"}
        </span>
        <span className="ml-auto" />
        <Button
          variant="ghost"
          size="icon-xs"
          title="Comment (on the whole item, or a section)"
          aria-label="Comment"
          onClick={() => start("comment")}
        >
          <Icon name="message-square-plus" />
        </Button>
        <Button
          variant="ghost"
          size="xs"
          title="Suggest a change: select the passage in a section first"
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => start("suggestion")}
        >
          Suggest
        </Button>
      </div>

      {adding && (
        <div className="flex flex-col gap-1.5 rounded-md border p-2">
          <div className="flex items-center gap-1.5 text-sm">
            <b>{adding.kind === "suggestion" ? "Suggestion" : "Comment"}</b>
            <Pick
              label="Section"
              value={adding.section}
              onChange={(v) => setAdding({ ...adding, section: v })}
              options={[
                { value: "", label: adding.kind === "comment" ? "The whole item" : "Section…" },
                ...d.order.map((s) => ({ value: s, label: s })),
              ]}
            />
          </div>
          {adding.kind === "suggestion" && (
            <>
              <Textarea
                value={adding.quote}
                onChange={(e) => setAdding({ ...adding, quote: e.target.value })}
                placeholder="The passage as it is (exactly)"
                rows={2}
                aria-label="Instead of"
              />
              <Textarea
                value={adding.replace}
                onChange={(e) => setAdding({ ...adding, replace: e.target.value })}
                placeholder="What to put instead"
                rows={2}
                aria-label="Put"
              />
            </>
          )}
          <Textarea
            autoFocus={adding.kind === "comment"}
            value={adding.text}
            onChange={(e) => setAdding({ ...adding, text: e.target.value })}
            placeholder={adding.kind === "comment" ? "The note" : "Why (optional)"}
            rows={2}
            aria-label="Note"
          />
          <div className="flex gap-1.5">
            <Button size="sm" disabled={busy} onClick={() => saveNote(adding)}>
              Add to the draft round
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setAdding(null)}>
              Cancel
            </Button>
          </div>
        </div>
      )}

      {d.notes.map((n) => (
        <div key={n.id} className="flex flex-col gap-1 rounded-md border p-2">
          <div className="flex items-center gap-1.5 text-xs">
            <b>{n.kind === "suggestion" ? "Suggestion" : "Comment"}</b>
            {n.section && <span className="text-muted-foreground">{n.section}</span>}
            <span className="text-muted-foreground">on revision {n.item_rev}</span>
            <Button
              variant="ghost"
              size="icon-xs"
              className="ml-auto"
              title="Take it out"
              aria-label="Take it out"
              onClick={() =>
                void act(async () => {
                  await w.dropNote(ws, n.id)
                  return ""
                })
              }
            >
              <Icon name="x" />
            </Button>
          </div>
          <NoteBody {...n} />
        </div>
      ))}

      <div className="flex gap-1.5">
        <Button
          variant="outline"
          size="sm"
          disabled={busy || d.notes.length === 0}
          onClick={() => void send()}
          title="Write the draft round to Caster and mail it to the owners"
        >
          <Icon name="send" /> Send round
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={() => void signOff()}
          title="Approve this revision's text"
        >
          <Icon name="badge-check" /> {so && !d.stale_sign ? "Sign off again" : "Sign off"}
        </Button>
      </div>
      {status && <p className="text-muted-foreground text-xs">{status}</p>}

      {d.rounds.length > 0 && d.changed_since_round && (
        <p className="text-muted-foreground text-xs">Revised since round {d.rounds[0].round} was sent.</p>
      )}
      {d.rounds.map((r, i) => (
        <RoundBox key={r.round} r={r} open={i === 0} />
      ))}
    </Sec>
  )
}

function RoundBox({ r, open }: { r: WorkRound; open: boolean }) {
  return (
    <Collapsible defaultOpen={open} className="rounded-md border">
      <CollapsibleTrigger
        render={
          <Button variant="ghost" size="sm" className="w-full justify-start">
            <Icon name="chevron-right" />
            Round {r.round} · {r.sent_at ? ago(r.sent_at) : "sent"} · {r.open ? `${r.open} open` : "all answered"}
          </Button>
        }
      />
      <CollapsibleContent className="flex flex-col gap-1.5 p-2 pt-0">
        {r.problem && <p className="text-warning text-xs">{r.problem}</p>}
        {r.notes.map((n) => (
          <div key={n.id} className="flex flex-col gap-1 rounded-md border p-2">
            <div className="flex items-center gap-1.5 text-xs">
              <b>{n.kind === "suggestion" ? "Suggestion" : "Comment"}</b>
              {n.section && <span className="text-muted-foreground">{n.section}</span>}
              <Badge variant={n.state === "open" ? "outline" : "secondary"} className="ml-auto">
                {STATE_LABEL[n.state] ?? n.state}
              </Badge>
            </div>
            <NoteBody {...n} />
            {n.reply && (
              <div className="flex gap-1 text-muted-foreground text-xs">
                ↳ <Markdown className="min-w-0 flex-1 text-xs wrap-anywhere">{n.reply}</Markdown>
              </div>
            )}
          </div>
        ))}
      </CollapsibleContent>
    </Collapsible>
  )
}
