# Workflows

Studio's home for designing user/agent/system workflows as Surface modules. A brief records the intended outcome, each actor's steps, and acceptance checks. The workflow designer uses the `workflows` slot; its external specialist folder is selected through the module-agent binding when available. This module does not ship a substitute specialist identity.

Create and save a draft, fill the three actors' steps and acceptance checks, mark it for review, then mark it ready after review. Only the owner can mark ready. Changed content must return to draft. Every save compares the revision, so stale pages and agent turns cannot silently overwrite a newer brief. Delegated writes also refuse to change a brief while its window is dirty or composing.

The server owns the SQLite briefs and publishes `workflows.changed`. The view uses registry components and refreshes its list on that event, retaining unsaved input and offering an explicit reload for stale revisions. HTTP writes are owner-only; agent/outside clients use the write-level `workflows_save` tool. `workflows_list`, `workflows_get`, and `workflows_handoff` are read-only.

A review handoff is a Markdown snapshot with the brief ID and revision, ready to copy into a Design request. Preparing it does not contact the design team or start work. The agent may use the existing `design_request` tool when authorized. Review readiness is not approval to deploy or merge an implementation.

Tests: `go test -race ./modules/workflows ./module/...`, plus the repository's page typecheck/lint, generated wiring check and CI.
