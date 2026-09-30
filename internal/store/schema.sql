-- Cards hold one Markdown body whose first line is the "# <title>" heading.
-- kind is "task" or "epic"; epic_id points to the epic a task belongs to.
CREATE TABLE IF NOT EXISTS tasks (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  repo        TEXT    NOT NULL,
  body        TEXT    NOT NULL,
  status      TEXT    NOT NULL,
  kind        TEXT    NOT NULL,
  priority    TEXT    NOT NULL,
  epic_id     INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_repo_status ON tasks(repo, status);
CREATE INDEX IF NOT EXISTS idx_tasks_epic ON tasks(epic_id);
