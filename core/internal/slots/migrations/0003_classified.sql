-- What the classifier made of each turn, in shadow: a JSON classify.Result, or '' when it
-- did not run. Kept beside the turn so its answers can be scored against what followed.
ALTER TABLE context_turns ADD COLUMN classified TEXT NOT NULL DEFAULT '';
