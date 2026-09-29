package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestInstallUserRegistersBothClients(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX shell shims")
	}
	shimDir := t.TempDir()
	logDir := t.TempDir()
	for _, name := range []string{"codex", "claude"} {
		script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$REGISTRATION_LOG/" + name + "\"\n"
		if err := os.WriteFile(filepath.Join(shimDir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("REGISTRATION_LOG", logDir)
	output := mustRun(t, "install")
	if !strings.Contains(output, "codex (user)") || !strings.Contains(output, "claude (user)") {
		t.Errorf("install output = %q", output)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"codex":  "mcp\nadd\nagentboard\n--\n" + binary + "\nserve\n",
		"claude": "mcp\nadd\n--scope\nuser\nagentboard\n--\n" + binary + "\nserve\n",
	} {
		got, err := os.ReadFile(filepath.Join(logDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s args = %q, want %q", name, got, want)
		}
	}
}

func TestInstallProjectPreservesConfigAndIsIdempotent(t *testing.T) {
	dir := setup(t)
	t.Chdir(dir)
	other := newDir(t)
	claudePath := filepath.Join(dir, ".mcp.json")
	codexPath := filepath.Join(dir, ".codex", "config.toml")
	if err := os.WriteFile(claudePath, []byte(`{"custom":true,"mcpServers":{"other":{"command":"other"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Dir(codexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte("model = \"example\"\n\n[mcp_servers.other]\ncommand = \"other\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "--repo", other, "install", "--scope", "project")
	if _, err := os.Stat(filepath.Join(other, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("--repo directory was changed: %v", err)
	}
	claudeData, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	var claudeConfig struct {
		Custom     bool                    `json:"custom"`
		MCPServers map[string]serverConfig `json:"mcpServers"`
	}
	if err := json.Unmarshal(claudeData, &claudeConfig); err != nil {
		t.Fatal(err)
	}
	if !claudeConfig.Custom || claudeConfig.MCPServers["other"].Command != "other" {
		t.Errorf("existing Claude settings lost: %s", claudeData)
	}
	codexData, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	var codexConfig struct {
		Model      string                  `toml:"model"`
		MCPServers map[string]serverConfig `toml:"mcp_servers"`
	}
	if err := toml.Unmarshal(codexData, &codexConfig); err != nil {
		t.Fatal(err)
	}
	if codexConfig.Model != "example" || codexConfig.MCPServers["other"].Command != "other" {
		t.Errorf("existing Codex settings lost: %s", codexData)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	for name, entry := range map[string]serverConfig{
		"Claude": claudeConfig.MCPServers["agentboard"],
		"Codex":  codexConfig.MCPServers["agentboard"],
	} {
		if entry.Command != binary || len(entry.Args) != 1 || entry.Args[0] != "serve" {
			t.Errorf("%s registration = %+v", name, entry)
		}
	}
	mustRun(t, "install", "--scope", "project")
	for path, want := range map[string]string{claudePath: string(claudeData), codexPath: string(codexData)} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("rerun changed %s: %v", path, err)
		}
	}
}

func TestInstallProjectRejectsConflictBeforeWriting(t *testing.T) {
	dir := setup(t)
	t.Chdir(dir)
	codexPath := filepath.Join(dir, ".codex", "config.toml")
	if err := os.Mkdir(filepath.Dir(codexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, []byte("[mcp_servers.agentboard]\ncommand = \"another\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "install", "--scope", "project"); err == nil || !strings.Contains(err.Error(), "already configured differently") {
		t.Errorf("conflict error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("Claude config written after conflict: %v", err)
	}
	if _, err := run(t, "install", "--scope", "invalid"); err == nil || !strings.Contains(err.Error(), "invalid scope") {
		t.Errorf("invalid scope error = %v", err)
	}
}
