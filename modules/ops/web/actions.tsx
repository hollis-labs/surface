import { Icon } from "@hollis-labs/surface-page/sdk"
import { useState } from "react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { message, type OpsClient, type Workstream } from "./api"

export function WorkstreamActions({ w, ops, refresh }: { w: Workstream; ops: OpsClient; refresh: () => void }) {
  const [open, setOpen] = useState(false)
  const [note, setNote] = useState("")
  const [requestID, setRequestID] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const pending = w.orchestration_request && !w.orchestration_request.message_id
  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    setError("")
    try {
      await fn()
      setOpen(false)
    } catch (e) {
      setError(message(e))
    } finally {
      setBusy(false)
      refresh()
    }
  }
  const canSend = !w.settled_at && (["planned", "parked", "queued", "in_flight"].includes(w.stage) || pending)
  const alreadySent = !!w.orchestration_request?.message_id
  return (
    <>
      {w.settled_at ? (
        <Button
          size="icon-xs"
          variant="ghost"
          className="size-6 text-muted-foreground hover:text-foreground"
          disabled={busy}
          title="Unsettle workstream"
          aria-label={`Unsettle ${w.title}`}
          onClick={(e) => {
            e.stopPropagation()
            void run(() => ops.unsettle(w.id, w.rev))
          }}
        >
          <Icon name="rotate-ccw" size={11} />
        </Button>
      ) : (
        w.stage === "complete" && (
          <Button
            size="icon-xs"
            variant="ghost"
            className="size-6 text-success hover:text-success"
            disabled={busy}
            title="Settle completed workstream"
            aria-label={`Settle ${w.title}`}
            onClick={(e) => {
              e.stopPropagation()
              void run(() => ops.settle(w.id, w.rev))
            }}
          >
            <Icon name="check-circle-2" size={11} />
          </Button>
        )
      )}
      {canSend && (
        <Button
          size="icon-xs"
          variant="ghost"
          className={`size-6 ${
            alreadySent
              ? "text-primary hover:text-primary"
              : pending
                ? "text-warning hover:text-warning"
                : "text-muted-foreground hover:text-foreground"
          }`}
          title={
            alreadySent
              ? "Sent to orchestration (click to send follow-up instructions)"
              : pending
                ? "Orchestration send pending (click to retry)"
                : `Send ${w.title} to orchestration`
          }
          aria-label={`Send ${w.title} to orchestration`}
          onClick={(e) => {
            e.stopPropagation()
            setNote(pending ? (w.orchestration_request?.note ?? "") : "")
            setRequestID(pending ? (w.orchestration_request?.request_id ?? "") : crypto.randomUUID())
            setError("")
            setOpen(true)
          }}
        >
          <Icon name="send" size={11} />
        </Button>
      )}
      {!open && error && (
        <span role="alert" className="text-destructive text-xs">
          {error}
        </span>
      )}
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!busy) setOpen(value)
        }}
      >
        <DialogContent onClick={(e) => e.stopPropagation()}>
          <DialogHeader>
            <DialogTitle>Send to orchestration</DialogTitle>
            <DialogDescription>
              Ask the Lead Orchestrator to sequence and launch {w.title} by current priorities, or their judgment, and
              queue it for execution.
            </DialogDescription>
          </DialogHeader>
          <Label htmlFor={`ops-note-${w.id}`}>Optional note</Label>
          <Textarea
            id={`ops-note-${w.id}`}
            value={note}
            maxLength={4096}
            disabled={busy || !!pending}
            onChange={(e) => setNote(e.target.value)}
          />
          {error && (
            <p role="alert" className="text-destructive text-sm">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button variant="outline" disabled={busy} onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button disabled={busy} onClick={() => void run(() => ops.send(w.id, w.rev, requestID, note))}>
              {busy ? "Sending…" : pending ? "Retry send" : "Send"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
