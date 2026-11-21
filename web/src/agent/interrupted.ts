/**
 * What the panel says and offers for an interrupted turn (the chat state's `interrupted`).
 * Pure, so it is tested without a page.
 */
import type { InterruptedOffer } from "@hollis-labs/surface-chat"
import type { RecoverAction } from "./api"

/** Why the offer is what it is, in a sentence. */
export function interruptedReason(offer: InterruptedOffer): string {
  if (offer.integrity === "blocked") {
    const why = offer.reason ? ` (${offer.reason})` : ""
    return offer.turn
      ? `This conversation failed its integrity checks${why}, so the interrupted turn cannot be resumed. Discard it, then reset the conversation to go on.`
      : `This conversation failed its integrity checks${why} and takes no new messages. Reset it to go on: the agent starts a new history and the old one is kept.`
  }
  return offer.reason || "The app stopped before the agent finished its last turn."
}

/** The actions on offer, in a fixed order. A blocked conversation offers only discard and reset. */
export function interruptedActions(offer: InterruptedOffer): RecoverAction[] {
  const order: RecoverAction[] = offer.integrity === "blocked" ? ["discard", "reset"] : ["resume", "discard", "reset"]
  return order.filter((a) => offer.actions.includes(a))
}
