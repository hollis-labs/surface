-- Seeded reflexes that keep the agent's state current (docs/agent.md, "Keeping state current").
-- INSERT OR IGNORE: a reflex of the same name the owner already has is left as it is, and a seed
-- the owner deletes later is not put back (a migration runs once).
INSERT OR IGNORE INTO reflexes (name, about, trigger_kind, trigger, action_kind, action, cooldown, enabled) VALUES
	('save-state',
	'After the host runner summarizes older turns (agent.compacted), remind the agent to bring its slot note and standing up to date.',
	'event', '{"topic": "agent.compacted"}',
	'remind', '{"body": "Older turns were just summarized. If anything durable changed, bring the slot note (context_note) and standing (standing_update) up to date before going on."}',
	0, 1);
INSERT OR IGNORE INTO reflexes (name, about, trigger_kind, trigger, action_kind, action, cooldown, enabled) VALUES
	('standing-rule',
	'When the owner''s message sounds like it sets a standing rule, remind the agent to record it with standing_update.',
	'text', '{"pattern": "(?i)(\\bfrom now on\\b|\\bgoing forward\\b|\\bstanding (rule|instruction)s?\\b|\\bmake (it|that|this) a rule\\b|\\b(don''t|don’t|do not) ever\\b|\\bnever again\\b|(^|[.!?;:\\n]\\s*|\\bplease )remember (that|to)\\b|(^|[.!?;:\\n]\\s*)(always\\b|never\\s+([a-ln-z]|m[a-hj-z]))|\\b(you should|you must|please|you''ll|you will) (always|never)\\b)"}',
	'remind', '{"body": "The owner may have set a standing rule. If so, update standing with standing_update in this turn and confirm in one line."}',
	0, 1);
