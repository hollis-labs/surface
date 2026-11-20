---
name: modes-and-presets
description: "Define presets (ways of working) and which one becomes a mode, in agent/steer v0.1.0 shape, without overlapping the shipped defaults."
---
# Modes and presets

A preset is a named bundle of behaviour. A mode is the preset the owner chose; **only the owner sets a mode**, and no tool can. A deliberate mode beats a classified preset.

```json
{"name": "review",
 "about": "Going over someone's work.",
 "instructions": "Lead with what is wrong, then what could be better. Quote the line.",
 "reflexes": ["cite-sources"],
 "slots": [], "tools": ["workbench_get", "workbench_validate"], "rules": [],
 "output_style": "brief", "delivery": "", "dispatch": ["subtask"]}
```

**Fields**
- `name`: same rule as reflexes.
- `instructions`: standing guidance while the preset is in force.
- `reflexes`: names defined in the same file.
- `slots`: slot names made active whatever is in front.
- `tools`: kept top of mind.
- `rules`: router rule ids.
- `output_style`: for example `brief` or `detailed`; the host interprets it.
- `delivery`: a delivery policy name, for example `hold` or `digest`; the host interprets it.
- `dispatch`: allowed strategies, from `subtask`, `council`, `seat`.

**Layering:** the shipped defaults are `general`, `focus`, `triage` and `research`. A module's presets replace a default of the same name; the app's replace a module's. Reuse a default name only when you mean to replace it.

**Guidance**
- Two to four presets per agent. Each must change behaviour you could observe, or it is noise.
- Put a mode's reflexes in its preset rather than enabling them everywhere.
- The classifier may *suggest* a preset (default threshold 0.8). Never design an agent that depends on auto-applied presets until replay shows precision.
