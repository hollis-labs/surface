# Sent message receipts

Done for you → Sent shows transport receipts and request progress independently.
A consumed message means it was taken from the mailbox; it does not acknowledge
or finish a request. An offline send or an upstream unacknowledged-request alert
also creates an informational card, even when this view is closed. Delivery or
request acknowledgement clears the offline card. Dismissed alerts stay dismissed
across reconnects and restarts.

The module uses its existing `assist.tether_command` / `assist.tether_url`
connection and filters to `assist.addresses`. Tracking runs independently of
`assist.sweep`. Settings are read on each reconnect. The browser refreshes from
`assist.*` events, without polling.

`0002_lifecycle.sql` preserves the old sent rows, adds account/task linkage, and
stores timestamped events and stream cursors. A batch and its cursor commit in
one transaction. Replay is idempotent. The first connection seeds the newest 25
matching Tether history events; subsequent connections use the saved environment
cursor and replay retained events after it. A retention gap or oversized catch-up re-seeds from recent history and leaves
a persistent warning that some receipts are unavailable. It cannot
reconstruct receipts Tether has purged. The view lists the newest 200 sent rows;
older records remain in SQLite. Legacy rows have no invented receipts.

The allowlist subscribes to `message.sent`, `message.queued`, `message.delivered`,
`message.read`, `message.consumed`, `wake.outcome`, `routing.reply_delivered`, and
`routing.reply_undeliverable`. Notify's wrapped `result.message` and wake receipt
are recorded immediately. Incoming replies correlate by `in_reply_to`; routing
outcomes correlate by `parent_id`. No message body or environment-wide event log
is stored by the tracker.

Request events are an additive integration with CW-20261120-0177:
`request.submitted`, `request.acknowledged`, `request.state_changed`,
`request.unacknowledged_alert`, and `request.closed`. The tolerant decoder is in
`lifecycle_decode.go`. It accepts `message_id`, or a known `task_id`, plus optional
`requester`, `recipient`, `state`/`status`, `at`, `summary`, and `reason`.
Task links may arrive in the send response or a later event. Unknown fields are
ignored. Request states normalize Torque's doing/cancelled aliases to in progress/
declined. Missing request events leave delivery tracking working; the module does
not infer acknowledgement, create a Torque task, or implement its own timeout.
The upstream request contract/send-path rollout remains required for those states.

Verification: `go test -race ./modules/assist`, `go vet ./...`, `go build ./...`,
`npm run typecheck`, and `npm run lint`. Tests cover notify wrappers, immediate
offline cards, duplicate/reordered receipts, address isolation, restart replay,
task correlation, request aliases, alert recovery, and stream failures/gaps.
