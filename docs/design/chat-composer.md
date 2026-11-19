# Chat composer candidate

Status: draft for design job d_03adcb6b (CW-20261118-0040). Owner review is pending.

Purpose: compose a message to an agent, with space for attachments and shortcuts.
This candidate demonstrates the input, not an upload service or command interpreter.

Anatomy: optional attachment row; growing multiline field; footer with attachment,
shortcut and submit actions; keyboard hint and delivery status outside the form.
Reuses PromptInput, InputGroupAddon, InputGroupButton and Button from @surface.

Variants: PromptInputSubmit accepts ready, sending and streaming. Sending is a
busy, disabled button; streaming retains the existing Stop action. Sizes retain
the registry defaults. There is no new palette, density or theme choice.

States: empty (Send disabled), typing, sending (draft retained and read-only),
disabled (all controls disabled), streaming (Stop enabled). Hover and focus use
the stock controls. Error is not part of this mock; a host must retain the draft
and expose a retryable error if delivery fails. The gallery uses an explicit
Simulate delivery action, with no network requests or automatic timeout.

Tokens: background, foreground, muted-foreground, input, ring and radius inherit
from the current Surface theme; text-xs for hints; gap-1/gap-2 and the existing
PromptInput spacing. The sending spinner honors reduced motion.

Accessibility: each form and textarea has a name. Hints use aria-describedby;
one status region per specimen announces simulated delivery. Tab order follows
attachment removal (when present), field, attachment, shortcut and submit.
Enter sends; Shift+Enter inserts a line; composing IME text is not submitted.
Shortcuts and simulated delivery return focus to the field. No overlays open.
Screen-reader announcements still require assistive-technology testing.

Narrow layouts: actions stay in the footer and hints wrap below it; the field
fills the available width and grows to the existing max-h-48 cap.

Use: pass a controlled draft and guard the host's submit callback against pending
or disabled state; keep the draft until delivery is confirmed. Use the gallery's
attachment/shortcut buttons only as fixture examples. Do not clear a real draft
on submit or treat these mock buttons as implemented upload/command features.

Reference rendering: gallery.html?theme=light#chat and gallery.html?theme=dark#chat
on the candidate branch. This is not a confirmed golden example.
