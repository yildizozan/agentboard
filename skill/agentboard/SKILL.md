---
name: agentboard
description: "Use when tracking, planning, resuming, or coordinating coding tasks on the agentboard MCP board. Check open tasks, claim work safely, record findings, and mark verified work done. Do not use for unrelated questions or mutate the board when only asked to inspect it."
---

# Agentboard task workflow

Use the `agentboard` MCP tools to coordinate work within a repository. The MCP server must already be connected. If it is unavailable, tell the user to run `agentboard install` (or `agentboard install --scope project`) and restart the agent client; do not write to the database directly.

1. Determine the absolute path of the working directory for the repository you are working in. Pass it as `cwd` to **every** `task_*` call. The server resolves the board from this path; do not substitute the agentboard source directory or omit `cwd`.
2. Call `task_list` with `cwd` before starting or resuming tracked work. It excludes `done` by default; use `status: "done"` when you need completed tasks. Avoid adding a duplicate task if the work is already listed.
3. To take an existing `todo` or `backlog` task, call `task_move` with its `id`, `status: "doing"`, and `from` equal to its current status. If it fails because the status changed, list again and do not claim work another agent took. Do not claim a task already `doing` without coordinating with its owner.
4. For new requested work that should be tracked, call `task_add` with a short `title`, useful `description`, and `status: "todo"`. Record ideas or later work as `backlog`. Adding a task does not claim it: move it to `doing` with `from: "todo"` before starting.
5. Use `task_update` to record findings, blockers, or a narrowed scope in the description. Use `task_move` for status changes. If blocked or interrupted, leave the task open and update its description with a useful handoff.
6. After the requested work is finished and checked, call `task_move` with `status: "done"` and `from: "doing"`. On a conflict, re-list before changing anything. Use `task_delete` only for tasks created by mistake, not for completed work.

When asked only to inspect or summarize the board, call `task_list` and report the results without modifying tasks. Never mark a task done just because code was written; first verify the outcome.