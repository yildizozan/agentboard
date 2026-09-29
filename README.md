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

Download a binary from [GitHub Releases](https://github.com/yildizozan/agentboard/releases)
and put `agentboard` (`agentboard.exe` on Windows) on your `PATH`:

| Platform              | Archive                                    |
|-----------------------|--------------------------------------------|
| Linux x86-64          | `agentboard_<version>_linux_amd64.tar.gz`  |
| Windows x86-64        | `agentboard_<version>_windows_amd64.zip`   |
| macOS Intel           | `agentboard_<version>_darwin_amd64.tar.gz` |
| macOS Apple Silicon   | `agentboard_<version>_darwin_arm64.tar.gz` |

Verify a download against `checksums.txt` from the same release.

Or build from source with Go 1.27+:

```bash
go install github.com/yildizozan/agentboard/cmd/agentboard@latest
```

`go install` does not build the web UI, so `agentboard board` shows a page
explaining how to build it. Release binaries include the UI.

## Connect an agent

Claude Code:

```bash
claude mcp add --scope user agentboard -- agentboard serve
```

Other MCP clients: run `agentboard serve` as a stdio server, for example:

```json
{
  "mcpServers": {
    "agentboard": { "command": "agentboard", "args": ["serve"] }
  }
}
```

One server serves every repository. Each tool call carries the agent's working
directory (`cwd`), and the board is chosen from it per call.

### Tools

| Tool          | Arguments                              | What it does                                         |
|---------------|----------------------------------------|------------------------------------------------------|
| `task_add`    | `cwd`, `title`, `description?`, `status?` | Add a task (default `backlog`)                     |
| `task_list`   | `cwd`, `status?`                       | List tasks; without `status`, `done` is left out     |
| `task_move`   | `cwd`, `id`, `status`, `from?`         | Move a task; with `from` it fails if the task moved  |
| `task_update` | `cwd`, `id`, `title?`, `description?`  | Change title or description                          |
| `task_delete` | `cwd`, `id`                            | Delete a task permanently                            |

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
  `backlog` for ideas and later work.
- Put findings or a narrowed scope into the task with `task_update`.
- When a task is finished, move it to `done`.
- Use this board for work that other agents or later sessions should see.
```

## CLI

The CLI works on the board of the current directory, or of `--repo <dir>`.

```text
agentboard add <title> [-d description] [-s status]
agentboard ls [-s status]
agentboard mv <id> <status> [--from status]
agentboard edit <id> [-t title] [-d description]
agentboard rm <id>
agentboard serve
agentboard board [--addr 127.0.0.1:7420]
agentboard --version
```

## Web board

```bash
agentboard board
```

It prints a URL such as `http://127.0.0.1:7420/?repo=...` with the current
repository preselected. Pick any board from the selector, drag cards between
columns, and add, edit or delete tasks. The page refreshes every two seconds,
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

```bash
go test -race ./...
```

```bash
npm --prefix web ci && npm --prefix web run build && go build ./cmd/agentboard
```

For UI work, run `agentboard board` and `npm --prefix web run dev` side by side;
Vite proxies `/api` to the running board.

Releases are built by [GoReleaser](https://goreleaser.com) when a `v*` tag is pushed.

## License

[MIT](LICENSE)
