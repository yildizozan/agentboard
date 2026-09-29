package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
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
	home := t.TempDir()
	t.Setenv("HOME", home)
	output := mustRun(t, "install")
	if !strings.Contains(output, "codex (user)") || !strings.Contains(output, "claude (user)") {
		t.Errorf("install output = %q", output)
	}
	binary := installedBinary(t)
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
	assertInstalledSkills(t, home)
}

// TestInstallUserReplacesExistingClaudeEntry reruns install over an existing registration,
// as after an upgrade: claude, unlike codex, refuses to add a name that already exists.
func TestInstallUserReplacesExistingClaudeEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX shell shims")
	}
	shimDir := t.TempDir()
	registry := t.TempDir()
	claude := `#!/bin/sh
entry="$REGISTRY/agentboard"
case "$1 $2" in
"mcp remove")
  [ "$3 $4 $5" = "--scope user agentboard" ] || { echo "unexpected remove: $*"; exit 3; }
  [ -f "$entry" ] || { echo "No user-scoped MCP server found with name: agentboard"; exit 1; }
  rm "$entry" ;;
"mcp add")
  [ -f "$entry" ] && { echo "MCP server agentboard already exists in user config"; exit 1; }
  printf '%s\n' "$@" > "$entry" ;;
*) exit 2 ;;
esac
`
	for name, script := range map[string]string{"claude": claude, "codex": "#!/bin/sh\nexit 0\n"} {
		if err := os.WriteFile(filepath.Join(shimDir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(registry, "agentboard"), []byte("old entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("REGISTRY", registry)
	t.Setenv("HOME", t.TempDir())

	for range 2 { // replacing must also work when the previous install left the entry
		if out, err := run(t, "install"); err != nil {
			t.Fatalf("install over an existing entry: %v\n%s", err, out)
		}
	}
	got, err := os.ReadFile(filepath.Join(registry, "agentboard"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(got), "--scope\nuser\nagentboard\n--\n"+installedBinary(t)+"\nserve\n") {
		t.Errorf("claude entry = %q, want the current binary", got)
	}
}

// installedBinary is the path install registers: the test binary with symlinks resolved.
func installedBinary(t *testing.T) string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	return binary
}

func TestInstallUserUpdatesSkillsWhenRegistrationFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX shell shims")
	}
	shimDir := t.TempDir()
	for _, name := range []string{"codex", "claude"} {
		if err := os.WriteFile(filepath.Join(shimDir, name), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := run(t, "install"); err == nil || !strings.Contains(err.Error(), "register with codex") {
		t.Errorf("registration error = %v", err)
	}
	assertInstalledSkills(t, home)
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
	assertInstalledSkills(t, dir)
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

func assertInstalledSkills(t *testing.T, dir string) {
	t.Helper()
	for _, client := range []string{".agents", ".claude"} {
		path := filepath.Join(dir, client, "skills", "agentboard", "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(data) != string(skillContent) {
			t.Errorf("skill at %s differs from bundled content", path)
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

func TestInstallProjectRefusesUnusableConfigWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		name, file, content, want string
	}{
		{"broken JSON", ".mcp.json", "{", "invalid JSON"},
		{"mcpServers not an object", ".mcp.json", `{"mcpServers": []}`, "invalid mcpServers"},
		{"other Claude entry", ".mcp.json", `{"mcpServers": {"agentboard": {"command": "another", "args": ["serve"]}}}`, "already configured differently"},
		{"broken TOML", ".codex/config.toml", "[[", "invalid TOML"},
		{"mcp_servers not a table", ".codex/config.toml", "mcp_servers = 1\n", "invalid mcp_servers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := setup(t)
			t.Chdir(dir)
			path := filepath.Join(dir, tc.file)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := run(t, "install", "--scope", "project"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("install error = %v, want %q", err, tc.want)
			}
			if data, _ := os.ReadFile(path); string(data) != tc.content {
				t.Errorf("%s changed to %q", tc.file, data)
			}
			for _, other := range []string{".mcp.json", ".codex/config.toml", ".claude/skills"} {
				if other == tc.file {
					continue
				}
				if _, err := os.Stat(filepath.Join(dir, other)); !os.IsNotExist(err) {
					t.Errorf("%s written although install failed: %v", other, err)
				}
			}
		})
	}
}

func TestInstallSkillsUserScope(t *testing.T) {
	home := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
	skillContent = []byte("first version")
	if err := installSkills(&cobra.Command{}, "user", ""); err != nil {
		t.Fatal(err)
	}
	assertInstalledSkills(t, home)
	skillContent = []byte("updated version")
	if err := installSkills(&cobra.Command{}, "user", ""); err != nil {
		t.Fatal(err)
	}
	assertInstalledSkills(t, home)
}

func TestInstallSkillsPreservesMatchingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires privileges on Windows")
	}
	dir := t.TempDir()
	skillContent = []byte("shared skill")
	codexDir := filepath.Join(dir, ".agents", "skills", "agentboard")
	claudeDir := filepath.Join(dir, ".claude", "skills")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "SKILL.md"), skillContent, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(claudeDir, "agentboard")
	if err := os.Symlink(filepath.Join("..", "..", ".agents", "skills", "agentboard"), link); err != nil {
		t.Fatal(err)
	}
	if err := installSkills(&cobra.Command{}, "project", dir); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("project symlink was replaced: %v", err)
	}
	skillContent = []byte("different skill")
	if err := installSkills(&cobra.Command{}, "project", dir); err != nil {
		t.Fatal(err)
	}
	assertInstalledSkills(t, dir)
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("update replaced the project symlink: %v", err)
	}
}

func TestWriteSkillRejectsDifferentSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "custom-skill.md")
	if err := os.WriteFile(target, []byte("personal skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	skillContent = []byte("bundled skill")
	if err := writeSkill(path); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("different symlink target should be preserved: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "personal skill" {
		t.Errorf("symlink target changed: %v", err)
	}
}
