/**
 * A generated surface, as a card. The envelope the server sent is decoded and checked by
 * @hollis-labs/surface-genui before anything is drawn; a surface that fails is shown as a card that
 * could not be drawn. The controller takes the owner's action to the host's gateway
 * (/api/genui), and the registry's GenuiSurface draws what the controller says.
 */
import {
  draftErrors,
  type Envelope,
  GenuiController,
  type GenuiTransport,
  type Input,
  parseRecovery,
} from "@hollis-labs/surface-genui"
import { useEffect, useMemo, useSyncExternalStore } from "react"
import { type GenuiPattern, GenuiSurface } from "@/components/ui/genui-surface"
import type { CardRendererProps } from "../cards/renderers"

async function send(method: string, path: string, body?: string, signal?: AbortSignal): Promise<string> {
  const res = await fetch(path, {
    method,
    body,
    signal,
    headers: body ? { "Content-Type": "application/json" } : undefined,
  })
  const text = await res.text()
  if (!res.ok) throw new Error(`${res.status} ${text}`)
  return text
}

/** How the controller reaches the host. The host decides everything; this only carries. */
const transport: GenuiTransport = {
  submit: (call, signal) => send("POST", "/api/genui/actions", call, signal),
  recover: async ({ actionId, functionCallId }, signal) =>
    parseRecovery(
      await send(
        "GET",
        `/api/genui/actions/${encodeURIComponent(actionId)}?functionCallId=${encodeURIComponent(functionCallId)}`,
        undefined,
        signal,
      ),
      functionCallId,
    ),
  // The owner pressing Confirm is the host's confirmation step here: it issues the proof.
  confirm: async ({ call }, signal) =>
    (JSON.parse(await send("POST", "/api/genui/confirm", call, signal)) as { proof: string }).proof,
}

function patternOf(envelope: Envelope): GenuiPattern {
  const root = envelope.data.createSurface.components[0]
  switch (root.component) {
    case "GenuiApproval":
      return {
        kind: "approval",
        title: root.props.title,
        summary: root.props.summary,
        changes: root.props.changes,
        choices: root.props.choices.map((c) => ({ label: c.label, risk: c.callFunction.args.genui_action.risk })),
      }
    case "GenuiForm":
      return {
        kind: "form",
        title: root.props.title,
        fields: root.props.fields,
        draft: envelope.data.createSurface.dataModel.draft,
        submitLabel: root.props.submit.label,
      }
    default:
      return {
        kind: "list_detail",
        title: root.props.title,
        items: root.props.items,
        selectedId: root.props.selectedId,
        detail: root.props.detail,
      }
  }
}

function actionIdOf(envelope: Envelope): string | undefined {
  const root = envelope.data.createSurface.components[0]
  if (root.component === "GenuiApproval") return root.props.choices[0]?.callFunction.args.genui_action.id
  if (root.component === "GenuiForm") return root.props.submit.callFunction.args.genui_action.id
  return undefined
}

export function GenuiCard({ card }: CardRendererProps) {
  // Decoding throws on anything the contract does not allow; the card's boundary shows that.
  const raw = useMemo(() => JSON.stringify(card.payload?.envelope ?? null), [card.payload])
  const controller = useMemo(() => new GenuiController(raw, transport), [raw])
  useEffect(() => () => controller.dispose(), [controller])
  const snap = useSyncExternalStore(controller.subscribe, controller.getSnapshot)
  const envelope = snap.envelope
  const root = envelope.data.createSurface.components[0]

  // A card that is already closed (answered earlier, or in another tab) reads what became
  // of its action from the host instead of offering it again. Reading never executes.
  const closed = !!card.status
  useEffect(() => {
    const action = actionIdOf(envelope)
    if (closed && action && controller.getSnapshot().phase === "ready") void controller.recover(action)
  }, [closed, controller, envelope])

  const observation = snap.observation
  const status =
    observation?.kind === "known"
      ? observation.outcome.status
      : observation?.kind === "unknown"
        ? observation.status
        : closed && card.status !== "submitted"
          ? card.status
          : undefined
  const select =
    root.component === "GenuiListDetail" && !closed
      ? (id: string) =>
          void send(
            "POST",
            `/api/genui/surfaces/${encodeURIComponent(envelope.id)}/select`,
            JSON.stringify({ id }),
          ).catch(() => {})
      : undefined
  return (
    <GenuiSurface
      pattern={patternOf(envelope)}
      view={root.props.state}
      phase={closed && snap.phase === "ready" ? "terminal" : snap.phase}
      status={status}
      message={snap.message}
      confirmation={
        observation?.kind === "known" && observation.outcome.status === "needs_confirmation"
          ? observation.outcome.confirm.what
          : undefined
      }
      canConfirm={snap.canConfirm}
      validate={(draft) =>
        root.component === "GenuiForm" ? { ...draftErrors(root.props.fields, draft as Input) } : {}
      }
      onChoose={(index, draft) => void controller.submit(index, draft as Input | undefined).catch(() => {})}
      onConfirm={() => void controller.confirm()}
      onCheck={() => void controller.recover()}
      onSelect={select}
    />
  )
}
