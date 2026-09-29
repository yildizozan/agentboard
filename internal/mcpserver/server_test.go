package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yildizozan/agentboard/internal/store"
)

// connect starts the server over in-memory transports and returns a client session.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "agentboard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := New(s, "test").Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// workDir returns a fresh non-git directory standing in for an agent's cwd.
func workDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "work")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// call invokes a tool and returns its text output and error flag.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	var texts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			texts = append(texts, tc.Text)
		}
	}
	return strings.Join(texts, "\n"), res.IsError
}

// mustCall fails the test when the tool reports an error.
func mustCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	out, isErr := call(t, cs, name, args)
	if isErr {
		t.Fatalf("%s(%v) failed: %s", name, args, out)
	}
	return out
}

func TestListToolsExposesCwdAndSchemas(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		raw, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		if _, ok := schema.Properties["cwd"]; !ok || !slices.Contains(schema.Required, "cwd") {
			t.Errorf("%s: cwd must be a required property, schema = %s", tool.Name, raw)
		}
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
	}
	for _, want := range []string{"task_add", "task_list", "task_move", "task_update", "task_delete"} {
		if !slices.Contains(names, want) {
			t.Errorf("tool %s missing from %v", want, names)
		}
	}
}

func TestAddAndList(t *testing.T) {
	cs := connect(t)
	dir := workDir(t)
	if out := mustCall(t, cs, "task_add", map[string]any{"cwd": dir, "title": "Fix login"}); out != "#1 [backlog] Fix login" {
		t.Errorf("task_add = %q", out)
	}
	mustCall(t, cs, "task_add", map[string]any{"cwd": dir, "title": "Ship it", "status": "done"})
	mustCall(t, cs, "task_add", map[string]any{"cwd": dir, "title": "Now", "status": "doing", "description": "details"})

	if out := mustCall(t, cs, "task_list", map[string]any{"cwd": dir}); out != "#1 [backlog] Fix login\n#3 [doing] Now" {
		t.Errorf("task_list = %q", out)
	}
	if out := mustCall(t, cs, "task_list", map[string]any{"cwd": dir, "status": "done"}); out != "#2 [done] Ship it" {
		t.Errorf("task_list done = %q", out)
	}
}

func TestListEmptyAndRepoIsolation(t *testing.T) {
	cs := connect(t)
	a, b := workDir(t), workDir(t)
	mustCall(t, cs, "task_add", map[string]any{"cwd": a, "title": "only in a"})
	if out := mustCall(t, cs, "task_list", map[string]any{"cwd": b}); out != "no tasks" {
		t.Errorf("task_list in other repo = %q", out)
	}
}

func TestInvalidInputIsToolError(t *testing.T) {
	cs := connect(t)
	dir := workDir(t)
	cases := map[string]map[string]any{
		"bad status":    {"cwd": dir, "title": "x", "status": "later"},
		"blank title":   {"cwd": dir, "title": "  "},
		"relative cwd":  {"cwd": "relative/dir", "title": "x"},
		"missing cwd":   {"cwd": filepath.Join(dir, "nope"), "title": "x"},
		"no cwd at all": {"title": "x"},
	}
	for name, args := range cases {
		if out, isErr := call(t, cs, "task_add", args); !isErr {
			t.Errorf("%s: expected tool error, got %q", name, out)
		}
	}
}

func TestMoveWithFromConflict(t *testing.T) {
	cs := connect(t)
	dir := workDir(t)
	mustCall(t, cs, "task_add", map[string]any{"cwd": dir, "title": "claim me", "status": "todo"})
	if out := mustCall(t, cs, "task_move", map[string]any{"cwd": dir, "id": 1, "status": "doing", "from": "todo"}); out != "#1 [doing] claim me" {
		t.Errorf("task_move = %q", out)
	}
	out, isErr := call(t, cs, "task_move", map[string]any{"cwd": dir, "id": 1, "status": "doing", "from": "todo"})
	if !isErr || !strings.Contains(out, "doing") {
		t.Errorf("second claim = %q (isError=%v), want conflict naming current status", out, isErr)
	}
}

func TestUpdateAndDelete(t *testing.T) {
	cs := connect(t)
	dir := workDir(t)
	mustCall(t, cs, "task_add", map[string]any{"cwd": dir, "title": "old", "status": "todo"})
	if out := mustCall(t, cs, "task_update", map[string]any{"cwd": dir, "id": 1, "title": "new"}); out != "#1 [todo] new" {
		t.Errorf("task_update = %q", out)
	}
	if out, isErr := call(t, cs, "task_update", map[string]any{"cwd": dir, "id": 1}); !isErr {
		t.Errorf("empty task_update = %q, want tool error", out)
	}
	if out := mustCall(t, cs, "task_list", map[string]any{"cwd": dir}); out != "#1 [todo] new" {
		t.Errorf("task_list after update = %q", out)
	}
	if out := mustCall(t, cs, "task_delete", map[string]any{"cwd": dir, "id": 1}); out != "deleted #1" {
		t.Errorf("task_delete = %q", out)
	}
	if out := mustCall(t, cs, "task_list", map[string]any{"cwd": dir}); out != "no tasks" {
		t.Errorf("task_list after delete = %q", out)
	}
}

func TestOneServerServesSeveralReposWithoutLeaks(t *testing.T) {
	cs := connect(t)
	a, b := workDir(t), workDir(t)
	mustCall(t, cs, "task_add", map[string]any{"cwd": a, "title": "in a"})
	mustCall(t, cs, "task_add", map[string]any{"cwd": b, "title": "in b"})

	for name, args := range map[string]map[string]any{
		"task_move":   {"cwd": b, "id": 1, "status": "done"},
		"task_update": {"cwd": b, "id": 1, "title": "hijacked"},
		"task_delete": {"cwd": b, "id": 1},
	} {
		if out, isErr := call(t, cs, name, args); !isErr || !strings.Contains(out, "not found") {
			t.Errorf("%s on other repo's task = %q (isError=%v), want not found", name, out, isErr)
		}
	}
	if out := mustCall(t, cs, "task_list", map[string]any{"cwd": a}); out != "#1 [backlog] in a" {
		t.Errorf("repo a changed: %q", out)
	}
	if out := mustCall(t, cs, "task_list", map[string]any{"cwd": b}); out != "#2 [backlog] in b" {
		t.Errorf("repo b = %q", out)
	}
}
