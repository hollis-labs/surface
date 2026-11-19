/**
 * Approvals: the secret-request card (kind gatecrasher.secret). The other approvals use the
 * core's approval, choice and prompt cards.
 *
 * The value the owner types goes from this component straight to the producer's intake, as
 * the one form field the card names, and nowhere else. It lives only in this component's
 * state, is cleared once sent, and is never put in the card's answer, a route of this app, an
 * event, the desk's report or a log. The answer says only "provided" or "declined"; the
 * producer reports the secret received, and Gatecrasher closes the card.
 */
import type { CardProps, ModuleWeb } from "@hollis-labs/surface-page/sdk"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { CardActions, CardFrame, CardReceipt } from "@/components/ui/card-frame"
import { Input } from "@/components/ui/input"

const str = (v: unknown) => (typeof v === "string" ? v : "")

/**
 * Posts the value to the intake: a plain form post, so no response is read and nothing of
 * this page is sent with it. Exported for its test.
 */
export async function postSecret(postUrl: string, field: string, value: string): Promise<void> {
  const body = new URLSearchParams()
  body.set(field, value)
  await fetch(postUrl, {
    method: "POST",
    mode: "no-cors",
    credentials: "omit",
    referrerPolicy: "no-referrer",
    cache: "no-store",
    body,
  })
}

function SecretCard({ card, respond }: CardProps) {
  const p = card.payload ?? {}
  const [value, setValue] = useState("")
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState("")
  const open = !card.status
  const origin = str(p.origin)

  const provide = async () => {
    if (!value || busy) return
    setBusy(true)
    setProblem("")
    try {
      await postSecret(str(p.post_url), str(p.field) || "value", value)
      setValue("")
      await respond({ status: "submitted", data: { provided: true } })
    } catch {
      setProblem(`It could not be sent to ${origin}. Nothing was kept; try again.`)
    } finally {
      setBusy(false)
    }
  }
  const decline = () => void respond({ status: "submitted", data: { declined: true } })

  return (
    <CardFrame
      title={card.title || "A secret is asked for"}
      tone="danger"
      meta={<Badge variant="destructive">critical</Badge>}
      data-slot="card-gatecrasher-secret"
    >
      <div className="flex flex-col gap-2 text-sm">
        {str(p.summary) && <p>{str(p.summary)}</p>}
        <p className="text-muted-foreground">
          {str(p.head)} · for {str(p.requested_by)}
        </p>
        {open ? (
          <>
            <p className="text-muted-foreground">
              Sent only to <span className="font-mono">{origin}</span>, never to this app or its agent.
            </p>
            <Input
              type="password"
              autoComplete="off"
              spellCheck={false}
              aria-label="The secret"
              placeholder="The secret"
              value={value}
              disabled={busy}
              onChange={(e) => setValue(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && void provide()}
            />
            {problem && <p className="text-destructive">{problem}</p>}
            <CardActions>
              <Button variant="ghost" size="sm" disabled={busy} onClick={decline}>
                Decline
              </Button>
              <Button size="sm" disabled={busy || !value} onClick={() => void provide()}>
                Send to {origin}
              </Button>
            </CardActions>
          </>
        ) : (
          <CardReceipt state={card.status === "submitted" ? "submitted" : "expired"}>
            {card.status === "submitted" ? "Answered" : "Closed"}
          </CardReceipt>
        )}
      </div>
    </CardFrame>
  )
}

export default { views: {}, cards: { "gatecrasher.secret": SecretCard } } satisfies ModuleWeb
