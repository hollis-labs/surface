/**
 * How a card says what the person decided, and how it reads back what was decided before.
 *
 * The two directions are treated differently on purpose. What a card EMITS is a closed
 * union: answered or declined. There is no "partial": a half answer that claims the card
 * leaves it in a state nothing can complete. What a card READS is an open string, because
 * the server's recorded status is whatever the server says, including values this build
 * has never heard of; `classifyPriorResponse` is total over it, and an unknown value is
 * drawn honestly and stays locked rather than re-opening the card.
 *
 * A card never builds the wire response. It reports an outcome; the host adds the card's
 * id and sends it.
 *
 * Ported from @hollis-labs/kit-cards (hollis-labs/frontend-support), with the logic kept
 * and the styling left behind.
 */

/* ────────────────────────────── EMIT ────────────────────────────── */

/** One answer to one question. Mirrors the wire's `answers[]` element. */
export interface CardAnswer {
  readonly questionId: string
  readonly value: unknown
  /** The person took a suggestion as offered rather than composing their own. */
  readonly acceptedSuggestion?: boolean
  readonly note?: string
}

/** One decision about one item. Mirrors the wire's `decisions[]` element. */
export interface CardDecision {
  readonly itemId: string
  /** Domain vocabulary — `approve`, `skip`, `retry`. The card does not interpret it. */
  readonly action: string
  readonly note?: string
}

/**
 * What a card reports when the person is done with it.
 *
 * THREE SEPARATED CHANNELS, kept from the wire's own shape because the separation is
 * the good part: free-form `data`, question/answer pairs, and per-item decisions are
 * genuinely different things, and collapsing them into one bag is what made the
 * previous generation of envelope payloads unreadable.
 *
 * `error` IS NOT HERE EITHER, and for a different reason than `partial`. The wire has
 * an `error` status, but it describes a HANDLER that failed — the server's account of
 * itself. A card is the person's side of the exchange, and the two things a person
 * can do are answer and decline.
 */
export type CardOutcome =
  | {
      readonly status: "submitted"
      readonly data?: Readonly<Record<string, unknown>>
      readonly answers?: readonly CardAnswer[]
      readonly decisions?: readonly CardDecision[]
    }
  | { readonly status: "canceled" }

/**
 * The seam. A host adds `v`, the envelope id and transport; the card supplies only
 * what happened.
 *
 * Returning a promise is optional and a card must not depend on it resolving — a
 * host that fires and forgets is a legitimate host. Cards drive their own busy state
 * from props, not from awaiting this.
 */
export type CardResponder = (outcome: CardOutcome) => void | Promise<void>

/* ────────────────────────────── READ ────────────────────────────── */

/**
 * What a card should draw, given whatever the wire recorded last.
 *
 * `open` COVERS BOTH "NOTHING YET" AND "A PARTIAL", which is the one place this
 * classification encodes protocol rather than reporting it — and it is the protocol's
 * own rule. `ResponseStatus.IsTerminal()` shipped in go-envelopes precisely so a
 * non-terminal status does not claim the envelope: a `partial` is replaced by the
 * next submission, so a card that finds one is still open for business. Deriving that
 * from the protocol beats each host reinventing it, which is what the function exists
 * for.
 */
export type PriorResponseState =
  | { readonly kind: "open" }
  /** Claimed, handler still running. Controls should be disabled, not hidden. */
  | { readonly kind: "pending" }
  | { readonly kind: "submitted" }
  | { readonly kind: "canceled" }
  /** The server says the ask lapsed. Terminal; the client never infers this from a clock. */
  | { readonly kind: "expired" }
  /** Replaced by a newer card. Terminal; the host may link the replacement. */
  | { readonly kind: "superseded" }
  /** The handler failed. Terminal on the wire; a host may still offer a retry. */
  | { readonly kind: "failed"; readonly status: string }
  /**
   * A value this build does not know. NOT an error, and not silently treated as
   * open: a card that re-enables its controls against an unknown terminal state
   * invites a second submission that 409s. Draw it, say so, and stay locked.
   */
  | { readonly kind: "unrecognized"; readonly status: string }

/**
 * Total over `string`. See the header for why the input is not a union.
 *
 * `cancelled` is the legacy British spelling still on the wire; go-envelopes keeps it
 * as a compatibility value and canonicalises it, so a card that only matched the
 * American spelling would show a cancelled envelope as open.
 */
export function classifyPriorResponse(status: string | null | undefined): PriorResponseState {
  if (status == null || status === "") return { kind: "open" }

  switch (status) {
    case "submitted":
      return { kind: "submitted" }
    case "canceled":
    case "cancelled":
      return { kind: "canceled" }
    case "expired":
      return { kind: "expired" }
    case "superseded":
      return { kind: "superseded" }
    case "error":
    case "failed":
      return { kind: "failed", status }
    case "handling":
      return { kind: "pending" }
    case "partial":
      return { kind: "open" }
    default:
      return { kind: "unrecognized", status }
  }
}

/**
 * Whether a card should still accept input.
 *
 * Expressed against the classification rather than against the raw string so there is
 * ONE place that knows which states are live. `unrecognized` is deliberately not
 * interactive — see the note on that variant.
 */
export function acceptsInput(state: PriorResponseState): boolean {
  return state.kind === "open"
}
