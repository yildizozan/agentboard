---
name: agentboard
description: "Use when tracking, planning, resuming, or coordinating coding tasks on the agentboard MCP board. Check open tasks, claim work safely, record findings, and mark verified work done. Do not use for unrelated questions or mutate the board when only asked to inspect it."
---

# Agentboard task workflow

Use the `agentboard` MCP tools to coordinate work within a repository. The MCP server must already be connected. If it is unavailable, tell the user to run `agentboard install` (or `agentboard install --scope project`) and restart the agent client; do not write to the database directly.

1. Determine the absolute path of the working directory for the repository you are working in. Pass it as `cwd` to **every** `task_*` call. The server resolves the board from this path; do not substitute the agentboard source directory or omit `cwd`.
2. Call `task_list` with `cwd` before starting or resuming tracked work. It prints one title line per task and excludes `done` by default; use `status: "done"` when you need completed tasks. Read a card's full body with `task_get` before working on it. Avoid adding a duplicate task if the work is already listed. If several cards already describe the same or closely related work, fold the extras into the one to keep with `task_merge` (`id` = card to keep, `source` = card to fold in; the source is deleted and its body appended). A source in `doing` is refused because an agent may be working on it. Afterwards read the kept card with `task_get` and tidy it with `task_update`.
3. To take an existing `todo` or `backlog` task, call `task_move` with its `id`, `status: "doing"`, and `from` equal to its current status. If it fails because the status changed, list again and do not claim work another agent took. Do not claim a task already `doing` without coordinating with its owner.
4. For new requested work that should be tracked, call `task_add` with a `body` in the card format below, `status: "todo"`, and the required `epic: <epic id>`. Choose an existing epic for its goal, or create one first and use the returned ID (see Epics and priority below). Record ideas or later work as `backlog`, also with an epic. Adding a task does not claim it: move it to `doing` with `from: "todo"` before starting.
5. Use `task_update` to record findings, blockers, or a narrowed scope in the body. It replaces the whole body: call `task_get` first, edit that text, and send all of it back so no section is lost. Use `task_move` for status changes. If blocked or interrupted, leave the task open and add a `## Handoff` section with a useful handoff.
6. After the requested work is finished and checked, call `task_move` with `status: "done"` and `from: "doing"`. On a conflict, re-list before changing anything. Use `task_delete` only for tasks created by mistake, not for completed work.

## Epics and priority

- Each card has a `kind`: `task` (default) or `epic`. Open an epic with `task_add` and `kind: "epic"` for the shared goal of the requested work, even when it currently has one task; its body holds the goal and the overall acceptance criteria.
- Every task must belong to exactly one epic on the same board. Pass `epic: <epic id>` in `task_add`; use `task_update` to move it to another epic. The parent cannot be removed (`epic: 0` is rejected). Epics have no parent and cannot be nested.
- Legacy tasks without an epic can still be read or deleted. Before updating, moving, or merging into one, choose or create a suitable epic and assign it with `task_update` (`id: <task id>`, `epic: <epic id>`). Do not create duplicate tasks to work around a missing parent.
- An epic with tasks cannot be deleted, including when all of its tasks are done. Move or delete its tasks first; merging it into another epic moves its tasks to the target.
- List one epic's tasks with `task_list` and `epic: <epic id>`. Move the epic itself like any card: `doing` while its tasks are being worked on, `done` only after all of them are done and the overall acceptance holds.
- Each card has a `priority`: `low`, `normal` (default) or `high`; columns list high first. Set `high` only for work that should be picked before the rest (a blocker, a broken build, an explicit request) and `low` for work that can wait; leave everything else `normal`. Change it with `task_update` and `priority`.
- The list line shows these fields: `#12 [doing] [high] Fix login bug (epic #3)`, `#3 [todo] [epic] Auth rewrite`; normal priority is not shown.

For an empty board, create the epic first. If that call returns ID `3`, the task creation uses that ID:

```json
{"tool":"task_add","arguments":{"cwd":"/work/repo","body":"# Login reliability","kind":"epic","status":"todo"}}
{"tool":"task_add","arguments":{"cwd":"/work/repo","body":"# Fix login token expiry","status":"todo","epic":3}}
```

## Card format

Each card is a single Markdown `body`.

- The first line is the card title as a level-1 heading: `# Fix login token expiry`. Keep it short and specific; the board preview and `task_list` show only this line.
- A plain first line is turned into the heading automatically. A first line starting with `##` or a blank title is rejected.
- Put everything else below the title using `##` sections. Use only the sections that carry information:

```markdown
# Fix login token expiry

## Context
Expired tokens are accepted for one extra second: `auth/middleware.go:42` compares with `<` instead of `<=`.

## Acceptance
- [ ] Token expiring at `now` is rejected
- [ ] Regression test in `auth/middleware_test.go`

## Findings
- `refresh.go` has the same comparison.

## Handoff
Fix done in middleware; `refresh.go` still open.
```

- Use `- [ ]` / `- [x]` checklists for steps and acceptance criteria, and tick them as you go.
- Use backticks for paths, commands, and identifiers so they stay readable in the rendered board.

When asked only to inspect or summarize the board, call `task_list` and report the results without modifying tasks. Never mark a task done just because code was written; first verify the outcome.
