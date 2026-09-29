# agentboard

A repo-scoped task board for coding agents. Agents track their work over
[MCP](https://modelcontextprotocol.io); you watch and steer it from a CLI or a
local web board.

Every repository gets its own board with four columns:

| Status    | Meaning                                   |
|-----------|-------------------------------------------|
| `backlog` | Idea or later work, not planned yet       |
| `todo`    | Planned, next up                          |
| `doing`   | Actively being worked on                  |
| `done`    | Finished                                  |

Boards live in one SQLite file (`~/.agentboard/agentboard.db`), so they survive
sessions and are shared by every agent on the machine. Moving a task can be a
compare-and-swap (`from`), so two agents cannot both claim the same task.

## Install

Download the binary for your platform from
[GitHub Releases](https://github.com/yildizozan/agentboard/releases), rename it to
`agentboard` (`agentboard.exe` on Windows) and put it on your `PATH`:

| Platform            | Binary                           |
|---------------------|----------------------------------|
| Linux x86-64        | `agentboard_linux_amd64`         |
| Windows x86-64      | `agentboard_windows_amd64.exe`   |
| macOS Apple Silicon | `agentboard_darwin_arm64`        |

```bash
curl -fLo agentboard https://github.com/yildizozan/agentboard/releases/latest/download/agentboard_darwin_arm64
chmod +x agentboard
```

Verify a download against `checksums.txt` from the same release.

Or build from source with Go 1.27+:

```bash
go install github.com/yildizozan/agentboard@latest
```

`go install` does not build the web UI, so `agentboard board` shows a page
explaining how to build it. Release binaries include the UI.

## Connect an agent

With Codex and Claude Code installed, register both at user scope (the default):

```bash
agentboard install
```

To register only in the current directory instead, run:

```bash
agentboard install --scope project
```

This writes `.codex/config.toml` for Codex and `.mcp.json` for Claude Code in
the directory where you run it, independent of `--repo`. Codex loads project
configuration only after you trust the project; Claude Code asks you to approve
project MCP servers. Re-running project install keeps matching entries and
reports a conflict rather than replacing a different `agentboard` entry.
Registration uses the absolute path of the installed `agentboard` binary, so
move the binary only after re-registering it.

See the [Codex MCP guide](https://learn.chatgpt.com/docs/extend/mcp) and
[Claude Code MCP guide](https://code.claude.com/docs/en/mcp) for manual setup.
Other MCP clients can run `agentboard serve` as a stdio server, for example:

```json
{
  "mcpServers": {
    "agentboard": { "command": "agentboard", "args": ["serve"] }
  }
}
```

One server serves every repository. Each tool call carries the agent's working
directory (`cwd`), and the board is chosen from it per call.

### Agent skill

The [agentboard skill](skill/agentboard/SKILL.md) is bundled in the
binary. `agentboard install` registers the MCP server and installs the skill
to `~/.agents/skills/agentboard/` for Codex and `~/.claude/skills/agentboard/`
for Claude Code on Linux, macOS, and Windows. With `--scope project`, both the
MCP configuration and skills go into the current directory instead. Run install
again after upgrading or moving the binary: it updates the installed skill and
replaces the user-scoped `agentboard` MCP entry of both clients with the
current binary. You can invoke the skill as `$agentboard` in Codex or `/agentboard` in
Claude Code.

### Tools

| Tool          | Arguments                      | What it does                                          |
|---------------|--------------------------------|-------------------------------------------------------|
| `task_add`    | `cwd`, `body`, `status?`       | Add a task (default `backlog`)                        |
| `task_list`   | `cwd`, `status?`               | List title lines; without `status`, `done` is left out |
| `task_get`    | `cwd`, `id`                    | Show a task's status line and full Markdown body      |
| `task_move`   | `cwd`, `id`, `status`, `from?` | Move a task; with `from` it fails if the task moved   |
| `task_update` | `cwd`, `id`, `body`            | Replace a task's body                                 |
| `task_delete` | `cwd`, `id`                    | Delete a task permanently                             |

### Cards

A card is one Markdown body. Its first line is the title as a level-1 heading
(`# Fix login bug`); a plain first line becomes that heading, and a first line
starting with `##` is rejected. `task_list`, `agentboard ls` and the board
columns show only the title; the board opens the rendered body on click.

```markdown
# Fix login bug

## Context
Token expiry uses `<` instead of `<=`.

## Acceptance
- [ ] Expired token is rejected
```

### Agent instructions

Agents only use the board when told to. Add this to the `AGENTS.md` or
`CLAUDE.md` of your projects:

```markdown
## Task board

Track work on the `agentboard` MCP server. Always pass your current working
directory as an absolute path in `cwd`.

- When you start, call `task_list` to see open work.
- Before working on a task, claim it with `task_move` (`status: "doing"`,
  `from`: its current status). If the call fails, another agent took it;
  pick another task.
- Record work you discover with `task_add`: `todo` for planned next steps,
  `backlog` for ideas and later work. The `body` is Markdown whose first
  line is the `# <title>` heading.
- Read a task with `task_get`; put findings or a narrowed scope into its body
  with `task_update`, which replaces the whole body.
- When a task is finished, move it to `done`.
- Use this board for work that other agents or later sessions should see.
```

## CLI

The CLI works on the board of the current directory, or of `--repo <dir>`.

```text
agentboard add <body|-> [-s status]
agentboard ls [-s status]
agentboard show <id>
agentboard mv <id> <status> [--from status]
agentboard edit <id> <body|->
agentboard rm <id>
agentboard serve
agentboard install [--scope user|project]
agentboard board [--addr 127.0.0.1:7420]
agentboard --version
```

## Web board

```bash
agentboard board
```

It prints the board URL of the current repository: the repository path is the
page path, such as `http://127.0.0.1:7420/Users/me/project`. Pick any board from the selector, drag cards between
columns, and add or delete tasks. Click a card title to read its rendered
Markdown body and edit it (Cmd/Ctrl+Enter saves, Esc leaves the editor). The page refreshes every two seconds,
so work done by agents shows up by itself. If an agent moved a card after the
page last refreshed, dropping that card is refused and the board reloads
instead of overwriting the agent's change.

The board has no authentication and only listens on loopback addresses.
Requests with a non-loopback `Host` header are rejected, and writes must be JSON,
so other websites open in your browser cannot change your boards.

## How boards are identified

- Inside a git repository, the board belongs to the repository: its root,
  subdirectories and all linked worktrees share one board. Submodules get
  their own board.
- Outside git, the board belongs to the directory itself.
- Symlinks are resolved, so a symlinked path opens the same board.

Set `AGENTBOARD_HOME` to keep the database somewhere other than `~/.agentboard`.

## Limitations

- Boards are keyed by path. If you move or re-clone a repository, its old tasks
  stay under the old path.
- Boards are local to the machine. There is no sync or sharing between machines.
- `git` must be installed; without it boards cannot be resolved.

## Development

The entry point is `main.go`; Cobra commands live in `cmd/root.go` and
`cmd/<command>.go`. Run `cobra-cli add <command>` from the repository root to
scaffold another subcommand.

```bash
go test -race ./...
```

```bash
npm --prefix web ci && npm --prefix web run build && go build .
```

For UI work, run `agentboard board` and `npm --prefix web run dev` side by side;
Vite proxies `/api` to the running board.

Releases are built by [GoReleaser](https://goreleaser.com) when a `v*` tag is pushed.

## License

[MIT](LICENSE)
