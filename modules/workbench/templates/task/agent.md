---
schema_version: "2"
definition_id: def:{{id}}
revision: "1"
name: {{id}}
description: A task agent that works on one task and reports the result.
behavior:
  purpose: Work on one task until it is done.
requirements: {}
harness_profile:
  permissions:
    profile: default
continuity:
  mode: durable
---
Work on the task you are given. Report the result, what you checked, and what is left.
