/** The one React piece: the state every answerable card shares. No markup. */
import { useState } from "react"
import { acceptsInput, classifyPriorResponse, type PriorResponseState } from "./response.ts"

export interface AnswerableInput {
  /** Whatever the server last recorded for the card, verbatim. */
  readonly status?: string | null
  /** The host is mid-submit. */
  readonly busy?: boolean
  /** The host got a 409: the card changed under the person. Their input is kept. */
  readonly stale?: boolean
}

export interface AnswerableState {
  readonly state: PriorResponseState
  /** The card is open and nothing blocks it from being answered. */
  readonly live: boolean
  /** Controls should be disabled: not live, stale, or a submit is running. */
  readonly disabled: boolean
  readonly busy: boolean
  /** The card was open in this mount and is now closed: the person, or the server, just
   * resolved it. A card that mounts closed never sets this, so a reload does not steal focus. */
  readonly transitioned: boolean
  /** Runs a responder with the busy flag set around it. */
  readonly run: (fn: () => void | Promise<void>) => Promise<void>
}

export function useAnswerable({ status, busy: hostBusy, stale }: AnswerableInput): AnswerableState {
  const state = classifyPriorResponse(status)
  const [localBusy, setLocalBusy] = useState(false)
  const busy = localBusy || hostBusy === true
  const live = acceptsInput(state)
  const [wasLive, setWasLive] = useState(live)
  if (live && !wasLive) setWasLive(true)
  const disabled = !live || busy || stale === true
  async function run(fn: () => void | Promise<void>) {
    if (disabled) return
    setLocalBusy(true)
    try {
      await fn()
    } finally {
      setLocalBusy(false)
    }
  }
  return { state, live, disabled, busy, transitioned: wasLive && !live, run }
}
