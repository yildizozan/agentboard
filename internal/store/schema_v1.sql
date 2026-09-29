-- Cards hold one Markdown body whose first line is the "# <title>" heading.
CREATE TABLE tasks (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  repo        TEXT    NOT NULL,
  body        TEXT    NOT NULL,
  status      TEXT    NOT NULL,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

CREATE INDEX idx_tasks_repo_status ON tasks(repo, status);
