package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	buildOnce   sync.Once
	builtBinary string
	buildErr    error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if builtBinary != "" {
		os.RemoveAll(filepath.Dir(builtBinary))
	}
	os.Exit(code)
}

// binary builds agentboard once per test run and returns its path.
func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "agentboard-bin")
		if err != nil {
			buildErr = err
			return
		}
		builtBinary = filepath.Join(dir, "agentboard")
		if runtime.GOOS == "windows" {
			builtBinary += ".exe"
		}
		if out, err := exec.Command("go", "build", "-ldflags", "-X main.version=9.9.9", "-o", builtBinary, ".").CombinedOutput(); err != nil {
			buildErr = &buildError{err: err, out: out}
		}
	})
	if buildErr != nil {
		t.Fatalf("build agentboard: %v", buildErr)
	}
	return builtBinary
}

type buildError struct {
	err error
	out []byte
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + string(e.out) }

func TestAgentboardSkillIsEmbedded(t *testing.T) {
	source, err := os.ReadFile("skill/agentboard/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(agentboardSkill) == 0 || !bytes.Equal(agentboardSkill, source) {
		t.Error("embedded skill differs from the repository skill")
	}
}

func TestBuiltBinaryInstallsProjectSkills(t *testing.T) {
	project := t.TempDir()
	install := exec.Command(binary(t), "install", "--scope", "project")
	install.Dir = project
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install from built binary: %v\n%s", err, output)
	}
	for _, client := range []string{".agents", ".claude"} {
		path := filepath.Join(project, client, "skills", "agentboard", "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(data, agentboardSkill) {
			t.Errorf("installed %s skill differs from binary", client)
		}
	}
}

// TestBuiltBinaryServesMCPOverStdio runs "agentboard serve" as a client would spawn it,
// so anything other than protocol messages on stdout breaks the session.
func TestBuiltBinaryServesMCPOverStdio(t *testing.T) {
	serve := exec.Command(binary(t), "serve")
	serve.Env = append(os.Environ(), "AGENTBOARD_HOME="+t.TempDir())
	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(ctx, &mcp.CommandTransport{Command: serve}, nil)
	if err != nil {
		t.Fatalf("connect to agentboard serve: %v", err)
	}
	defer cs.Close()

	if info := cs.InitializeResult().ServerInfo; info.Name != "agentboard" || info.Version != "9.9.9" {
		t.Errorf("server info = %+v, want agentboard 9.9.9", info)
	}
	cwd := t.TempDir()
	epic, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "task_add", Arguments: map[string]any{"cwd": cwd, "body": "# Stdio epic", "kind": "epic"}})
	if err != nil || epic.IsError {
		t.Fatalf("add epic: %+v %v", epic, err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "task_add", Arguments: map[string]any{"cwd": cwd, "body": "# over stdio", "epic": 1}})
	if err != nil || res.IsError {
		t.Fatalf("task_add = %+v, %v", res, err)
	}
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.HasSuffix(text, "[backlog] over stdio (epic #1)") {
		t.Errorf("task_add = %q", text)
	}
}
