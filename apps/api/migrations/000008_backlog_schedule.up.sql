-- Schedule/Gantt support for backlogs: the same start_date/due_on pair tasks
-- got in 000007, so a backlog can be plotted on a timeline of its own and its
-- tasks' completion read against a planned period.
--
-- Both stay app-only and are never synced to GitLab. A backlog is not a GitLab
-- milestone (nothing in the sync worker touches this table), and GitLab Issues
-- have no "start date" at all — the same reasoning as 000007's tasks.start_date.

ALTER TABLE backlogs ADD COLUMN start_date DATE;
ALTER TABLE backlogs ADD COLUMN due_on     DATE;
