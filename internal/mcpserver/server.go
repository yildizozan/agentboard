// Package mcpserver exposes the task board to coding agents over MCP.
package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yildizozan/agentboard/internal/repo"
	"github.com/yildizozan/agentboard/internal/store"
	"github.com/yildizozan/agentboard/internal/task"
)

// statusGuide is shared by the tool descriptions so agents use statuses consistently.
const statusGuide = "Statuses: backlog = idea or later work, not planned yet; todo = planned, next up; " +
	"doing = actively being worked on; done = finished."

// cwdInput is embedded in every tool input: the board is chosen per call from the agent's cwd.
type cwdInput struct {
	Cwd string `json:"cwd" jsonschema:"absolute path of your current working directory; selects the repository's board"`
}

type addInput struct {
	cwdInput
	Title       string `json:"title" jsonschema:"short task title"`
	Description string `json:"description,omitempty" jsonschema:"optional details"`
	Status      string `json:"status,omitempty" jsonschema:"initial status: backlog (default), todo, doing or done"`
}

type listInput struct {
	cwdInput
	Status string `json:"status,omitempty" jsonschema:"only list this status; by default every status except done"`
}

type moveInput struct {
	cwdInput
	ID     int64  `json:"id" jsonschema:"task id"`
	Status string `json:"status" jsonschema:"target status: backlog, todo, doing or done"`
	From   string `json:"from,omitempty" jsonschema:"status you expect the task to be in now; if it differs the move fails and reports the current status"`
}

type updateInput struct {
	cwdInput
	ID          int64   `json:"id" jsonschema:"task id"`
	Title       *string `json:"title,omitempty" jsonschema:"new title"`
	Description *string `json:"description,omitempty" jsonschema:"new description; an empty string clears it"`
}

type deleteInput struct {
	cwdInput
	ID int64 `json:"id" jsonschema:"task id"`
}

// New returns an MCP server whose tools read and write boards in s.
func New(s *store.Store) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "agentboard", Version: "dev"}, nil)
	h := handlers{store: s}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "task_add",
		Description: "Add a task to the board of the repository at cwd. Record work you plan to do or discover. " + statusGuide,
	}, h.add)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "task_list",
		Description: "List tasks on the board of the repository at cwd, ordered by status then id. Check it before starting work. " + statusGuide,
	}, h.list)
	mcp.AddTool(server, &mcp.Tool{
		Name: "task_move",
		Description: "Move a task to another status. When claiming a task pass from with the status you expect " +
			"(e.g. status=doing, from=todo): if another agent moved it first the call fails and reports the current status. " +
			"Move tasks to done when finished. " + statusGuide,
	}, h.move)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "task_update",
		Description: "Change a task's title or description, for example to record findings or narrow the scope. Status changes use task_move.",
	}, h.update)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "task_delete",
		Description: "Delete a task permanently. Prefer moving finished work to done; delete only tasks created by mistake.",
	}, h.delete)
	return server
}

type handlers struct {
	store *store.Store
}

func (h handlers) add(ctx context.Context, _ *mcp.CallToolRequest, in addInput) (*mcp.CallToolResult, any, error) {
	repoKey, err := resolve(in.Cwd)
	if err != nil {
		return nil, nil, err
	}
	status := task.Backlog
	if in.Status != "" {
		if status, err = task.ParseStatus(in.Status); err != nil {
			return nil, nil, err
		}
	}
	tk, err := h.store.Add(ctx, repoKey, in.Title, in.Description, status)
	if err != nil {
		return nil, nil, err
	}
	return text(tk.String()), nil, nil
}

func (h handlers) list(ctx context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, any, error) {
	repoKey, err := resolve(in.Cwd)
	if err != nil {
		return nil, nil, err
	}
	statuses := task.ActiveStatuses()
	if in.Status != "" {
		st, err := task.ParseStatus(in.Status)
		if err != nil {
			return nil, nil, err
		}
		statuses = []task.Status{st}
	}
	tasks, err := h.store.List(ctx, repoKey, statuses)
	if err != nil {
		return nil, nil, err
	}
	if len(tasks) == 0 {
		return text("no tasks"), nil, nil
	}
	lines := make([]string, len(tasks))
	for i, tk := range tasks {
		lines[i] = tk.String()
	}
	return text(strings.Join(lines, "\n")), nil, nil
}

func (h handlers) move(ctx context.Context, _ *mcp.CallToolRequest, in moveInput) (*mcp.CallToolResult, any, error) {
	st, err := task.ParseStatus(in.Status)
	if err != nil {
		return nil, nil, err
	}
	p := task.Patch{Status: &st}
	if in.From != "" {
		from, err := task.ParseStatus(in.From)
		if err != nil {
			return nil, nil, err
		}
		p.From = &from
	}
	return h.patch(ctx, in.Cwd, in.ID, p)
}

func (h handlers) update(ctx context.Context, _ *mcp.CallToolRequest, in updateInput) (*mcp.CallToolResult, any, error) {
	return h.patch(ctx, in.Cwd, in.ID, task.Patch{Title: in.Title, Description: in.Description})
}

// patch applies p to a task of the board at cwd; move and update share it.
func (h handlers) patch(ctx context.Context, cwd string, id int64, p task.Patch) (*mcp.CallToolResult, any, error) {
	repoKey, err := resolve(cwd)
	if err != nil {
		return nil, nil, err
	}
	tk, err := h.store.Update(ctx, repoKey, id, p)
	if err != nil {
		return nil, nil, err
	}
	return text(tk.String()), nil, nil
}

func (h handlers) delete(ctx context.Context, _ *mcp.CallToolRequest, in deleteInput) (*mcp.CallToolResult, any, error) {
	repoKey, err := resolve(in.Cwd)
	if err != nil {
		return nil, nil, err
	}
	if err := h.store.Delete(ctx, repoKey, in.ID); err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("deleted #%d", in.ID)), nil, nil
}

// resolve maps the agent's cwd to its board key.
func resolve(cwd string) (string, error) {
	key, err := repo.Resolve(cwd)
	if err != nil {
		return "", fmt.Errorf("cwd: %w", err)
	}
	return key, nil
}

func text(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}
