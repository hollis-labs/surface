-- An approval names the pull request too: two pull requests can share one head commit.
ALTER TABLE jobs ADD COLUMN approval_pr TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN approved_pr TEXT NOT NULL DEFAULT '';
