/**
 * Approvals: the secret-request card (kind approvals.secret). The other approvals use the
 * core's approval, choice and prompt cards.
 *
 * The value the owner types goes from this component straight to the producer's intake, as
 * the one form field the card names, and nowhere else. It lives only in this component's
 * state, is cleared once sent, and is never put in the card's answer, a route of this app, an
 * event, the desk's report or a log. The card's answer is only "declined"; on a send the
 * producer reports the secret received, and Gatecrasher closes the card.
 */
import { ApiError, type CardProps, type ModuleWeb } from "@hollis-labs/surface-page/sdk"
import { useEffect, useState } from "react"
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

interface Form {
  post_url: string
  origin: string
  field: string
}

function SecretCard({ card, respond, api }: CardProps) {
  const p = card.payload ?? {}
  const [form, setForm] = useState<Form | null>(null)
  const [refused, setRefused] = useState(false)
  const [value, setValue] = useState("")
  const [busy, setBusy] = useState(false)
  const [sent, setSent] = useState(false)
  const [problem, setProblem] = useState("")
  const [attempt, setTry] = useState(0)
  const [late, setLate] = useState(false)
  const open = !card.status

  // No confirmation a while after a send: the owner may send again.
  useEffect(() => {
    if (!sent) return
    const t = setTimeout(() => setLate(true), 30_000)
    return () => clearTimeout(t)
  }, [sent])

  // Where the form posts comes from this module, which keeps it from Gatecrasher, and only
  // for a card the module showed: never from the card's payload, which others can edit.
  // biome-ignore lint/correctness/useExhaustiveDependencies: attempt re-runs the fetch after the server did not answer
  useEffect(() => {
    if (!open) return
    let live = true
    api
      .get<Form>(`form/${encodeURIComponent(card.id)}`)
      .then((f) => live && setForm(f))
      .catch((e: unknown) => {
        if (!live) return
        if (e instanceof ApiError && e.status === 404) setRefused(true)
        else setTimeout(() => live && setTry((n) => n + 1), 3000) // the server did not answer: ask again
      })
    return () => {
      live = false
    }
  }, [api, card.id, open, attempt])

  const provide = async () => {
    if (!form || !value || busy) return
    setBusy(true)
    setProblem("")
    try {
      await postSecret(form.post_url, form.field, value)
      setValue("")
      // A no-cors post cannot read the intake's answer: the card closes when the producer
      // reports the secret received, and Gatecrasher says so.
      setSent(true)
    } catch {
      setProblem(`It could not be sent to ${form.origin}. Nothing was kept; try again.`)
    } finally {
      setBusy(false)
    }
  }
  const decline = () => {
    setValue("")
    void respond({ status: "submitted", data: { declined: true } })
  }

  return (
    <CardFrame
      title={card.title || "A secret is asked for"}
      tone="danger"
      meta={<Badge variant="destructive">critical</Badge>}
      data-slot="card-approvals-secret"
    >
      <div className="flex flex-col gap-2 text-sm">
        {str(p.summary) && <p>{str(p.summary)}</p>}
        <p className="text-muted-foreground">
          {str(p.head)} · for {str(p.requested_by)}
        </p>
        {!open ? (
          <CardReceipt state={card.status === "submitted" ? "submitted" : "expired"}>
            {card.status === "submitted" ? "Answered" : "Closed"}
          </CardReceipt>
        ) : refused ? (
          <p className="text-destructive">
            This card was not shown by the Approvals module, or is no longer open. Nothing will be sent from it.
          </p>
        ) : !form ? (
          <p className="text-muted-foreground">…</p>
        ) : sent ? (
          <p className="text-muted-foreground">
            Sent to <span className="font-mono">{form.origin}</span>. Waiting for it to confirm; this card closes when
            it does.
            {late && (
              <Button
                variant="link"
                size="sm"
                onClick={() => {
                  setSent(false)
                  setLate(false)
                }}
              >
                No confirmation yet: send again
              </Button>
            )}
          </p>
        ) : (
          <>
            <p className="text-muted-foreground">
              Sent only to <span className="font-mono">{form.origin}</span>, never to this app or its agent.
            </p>
            <Input
              type="password"
              autoComplete="new-password"
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
                Send to {form.origin}
              </Button>
            </CardActions>
          </>
        )}
      </div>
    </CardFrame>
  )
}

export default { views: {}, cards: { "approvals.secret": SecretCard } } satisfies ModuleWeb
