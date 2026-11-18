-- A pull request merged on GitHub that is not the approved commit: said loudly, once.
ALTER TABLE jobs ADD COLUMN merge_warning TEXT NOT NULL DEFAULT '';
