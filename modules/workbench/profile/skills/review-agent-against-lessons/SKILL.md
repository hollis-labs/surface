---
name: review-agent-against-lessons
description: "Review an agent before calling it done, against the lessons from Surface Desk, Basalisk and Nanite, and list what to tune first."
---
# Review an agent against the lessons

Run this as the `reviewer` variant, or yourself, on the finished agent. Report findings worst first, each with the file, the problem and a one-line fix.

**Checklist**
1. **Identity:** agent.md says who it is and what it never does in under about 400 words, and stands alone without skills.
2. **Context size:** `context/` is small. Every file there is paid for on every turn. Reference material goes in skills.
3. **Cache:** nothing per-turn in agent.md or context; per-turn facts come from the slot or the message.
4. **Noise:** background work reports through delivery (merge, digest, file), not as chat chatter. Questions go through `card_ask`, one at a time.
5. **Desk etiquette:**
   - a reason on every window it opens;
   - transient versus deliverable windows;
   - its own transient windows closed;
   - never steals focus from unsaved work;
   - never works around a refusal.
6. **Reflexes:**
   - each one guards an invariant;
   - cooldowns on common words;
   - no `halt` on focus or event triggers;
   - mode reflexes listed in presets, not enabled everywhere.
7. **Modes:** few, each observably different, set only by the owner.
8. **Credentials:** none literal; references only.
9. **Writes:** the agent proposes and the owner approves. Nothing loosens a `write` or `confirm` level.
10. **Evals:**
    - each case tests one behaviour;
    - at least one refuse-or-ask case;
    - graders that catch the likely wrong answer;
    - offline scripts present.
11. **Pending:** anything relying on Nanite 0.7.0 or Surface profiles is marked as such.

End with "Tune first:" and at most three items. Pick the ones the owner will notice in the first hour.
