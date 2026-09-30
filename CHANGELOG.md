# Changelog

## 0.2.0 — 2026-09-30

### Changed

- Every task requires exactly one epic on the same board in CLI, MCP and HTTP.
- Tasks can move to another epic, but their parent cannot be removed.
- Epics with tasks cannot be deleted, including completed tasks. Move or delete
  the tasks first; merging epics continues to move their tasks to the target.
- The web editor requires an epic for tasks and starts with epic creation when
  a board has no epics. Top-level epics remain editable without a parent.

### Upgrade

- Create an epic before adding tasks and pass its id with CLI `--epic` or API/MCP
  `epic`. Calls that previously omitted it or used `epic: 0` must change.
- Older tasks without an epic remain readable and deletable; assign a parent
  before editing or moving them. No automatic grouping or deletion is performed.
- Restart board and MCP processes after upgrading.
