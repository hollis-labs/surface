# Lessons (Surface Desk, Basalisk, Nanite)

What the owner has asked for, and what failed. Sources are in brackets.

- **Keep orchestration out of the chat.** The owner wants conversation, results and decision cards, not activity streams. "I don't want to see any of that activity." Background work reports through delivery (merge, digest, file), not as chat noise. [Basalisk Bas design 2026-11-14]
- **The agent always knows.** It uses the owner's own tools (Planner, Torque, Tesseract, notes) as its store, and checks they are current before answering "what is in flight". [Bas design]
- **Switching context must not change who the agent is.** Slots change what it has in mind and which tools are top of mind, never its identity or standing instructions. [Bas design; Surface agent.md]
- **Keep the prompt cache stable.** Per-turn context goes in front of the owner's message, not in the system prompt. Changing system blocks every turn broke the cache and drove up cost. [Nanite direction 2026-11-20: why Surface Desk left Nanite]
- **Watch total request size.** Compaction that ignored about 226 tool schemas let requests reach about 230k tokens and stall. Keep context files small, and keep top-of-mind tools to the few that matter. [same]
- **An over-budget block must warn, never vanish silently.** [same]
- **Reflexes are for invariants; instructions are for judgment.** Code supplies new sensors and verbs; config chooses which rules, thresholds and wording. Close gaps with limits, one base prompt and skills, not more reflexes. [Nanite decisions 2026-09-19 and 2026-11-20]
- **Classifier: shadow or suggest first.** Turn on auto only after replay shows precision. Default suggest thresholds: slot 0.7, preset 0.8. [Nanite steering; Surface classifier]
- **Desk etiquette won the ADE audit.** Window provenance, never stealing focus from unsaved work, holding changes while the owner types, attention from events only, and unanswered meaning lapsed, not declined. [Surface ADE audit 2026-11-18; desk rules]
- **Default reflex families worth having:** a loop and drift monitor (steer, then constrain, then stop), auto-retry for classified errors, watches and self-follow-ups, armed-not-run for risky commands. [ADE audit, gap 8]
- **Card lifecycle:**
  - supersede on comment;
  - edit before approve;
  - approval bound to a hash;
  - a per-turn budget for questions;
  - unanswered is not declined.

  [ADE audit, gap 4]
- **Mail from the owner's address carries his authority.** An assistant acting for him marks `on_behalf_of` and logs every action before doing it ("Done for you"). [Bas design]
- **Tuning loop:** "that was noise" and "always tell me" become rules. The agent edits its own rules on request, asking first and showing the change. What config cannot do goes to the owning developer as a task. [Bas design]
- **Live evals cost money:** estimate first, run only when the owner asks, and compare runs case by case. [Workbench README]
