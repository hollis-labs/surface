---
name: tune-front-desk
description: "Choose front-desk intake and delivery settings for an agent (Nanite chatrunner, 0.7.0) so background work reaches the owner as results, not noise."
---
# Tune the front desk

The front desk sits between the owner and the chat agent. **Intake** splits a multi-part message into a plan. **Work** runs the items as background subtasks. **Delivery** paces the results. A topic ledger tracks what is in flight. It is on Nanite main (0.7.0); Surface wires it into the app. For a profile, write the settings in the design, not a file.

**Intake** (`DeskOptions.Intake.Mode`):
- `off`;
- `shadow`: plans are recorded, nothing changes. Start here.
- `suggest`: a plan card the owner accepts, edits or declines.
- `auto`: a confident plan (`AutoConfidence` 0.8 by default) dispatches before the turn.

Planning uses rules, or the agent's `desk` model when `PlanWithModel` is on. Plan item kinds:
- `answer`: left to the chat agent;
- `research` and `analysis`: run as `research` subtasks;
- `council` and `delegate`: run as `worker`, or a host `Delegate` hands them to an owning seat;
- `question`: goes to the owner.

**Delivery policy** (defaults, first match wins):
- **now:** the owner's focus, urgent items, items the owner asked for at once, items waiting on the owner;
- **merge:** results on a topic within 20s become one message;
- **hold:** while a question on the topic is open, at most 30m;
- **digest:** progress and notices every 30m, at most 10 items;
- **file:** no message.

Critical items and questions always go out now. While the owner types, everything else waits.

**How to choose**
- Owner-facing assistant: intake `suggest`; results `merge`; progress `digest`; notices `file`. "Keep orchestration out of the chat."
- An agent the owner watches closely while building: intake `shadow` until a week of replay shows the plans are right.
- Never `auto` for a new agent.
