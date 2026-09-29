package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the root command with args and returns its combined output.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// mustRun fails the test when the command returns an error.
func mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := run(t, args...)
	if err != nil {
		t.Fatalf("agentboard %v: %v\n%s", args, err, out)
	}
	return out
}

// setup isolates the data dir and returns a fresh, non-git working directory.
func setup(t *testing.T) string {
	t.Helper()
	t.Setenv(homeEnv, t.TempDir())
	return newDir(t)
}

func newDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "work")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHelpShowsRepoFlag(t *testing.T) {
	out := mustRun(t, "--help")
	for _, want := range []string{"agentboard", "--repo", "add", "ls"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q:\n%s", want, out)
		}
	}
}

func TestAddDefaultsToBacklog(t *testing.T) {
	dir := setup(t)
	out := mustRun(t, "--repo", dir, "add", "Fix login")
	if out != "#1 [backlog] Fix login\n" {
		t.Errorf("add output = %q", out)
	}
}

func TestAddWithStatusAndDescription(t *testing.T) {
	dir := setup(t)
	out := mustRun(t, "--repo", dir, "add", "Write docs", "-d", "README first", "-s", "todo")
	if out != "#1 [todo] Write docs\n" {
		t.Errorf("add output = %q", out)
	}
}

func TestAddRejectsInvalidInput(t *testing.T) {
	dir := setup(t)
	if _, err := run(t, "--repo", dir, "add", "x", "-s", "later"); err == nil || !strings.Contains(err.Error(), "backlog, todo, doing, done") {
		t.Errorf("invalid status error = %v", err)
	}
	if _, err := run(t, "--repo", dir, "add", "   "); err == nil {
		t.Error("blank title accepted")
	}
	if _, err := run(t, "--repo", dir, "add"); err == nil {
		t.Error("missing title accepted")
	}
}

func TestLsHidesDoneByDefault(t *testing.T) {
	dir := setup(t)
	mustRun(t, "--repo", dir, "add", "open task", "-s", "doing")
	mustRun(t, "--repo", dir, "add", "finished", "-s", "done")

	if out := mustRun(t, "--repo", dir, "ls"); out != "#1 [doing] open task\n" {
		t.Errorf("ls = %q", out)
	}
	if out := mustRun(t, "--repo", dir, "ls", "-s", "done"); out != "#2 [done] finished\n" {
		t.Errorf("ls -s done = %q", out)
	}
	if _, err := run(t, "--repo", dir, "ls", "-s", "later"); err == nil {
		t.Error("ls accepted invalid status")
	}
}

func TestLsIsolatesRepos(t *testing.T) {
	a := setup(t)
	b := newDir(t)
	mustRun(t, "--repo", a, "add", "in a")
	if out := mustRun(t, "--repo", b, "ls"); out != "" {
		t.Errorf("ls in other repo = %q, want empty", out)
	}
}

func TestRepoDefaultsToWorkingDirectory(t *testing.T) {
	dir := setup(t)
	t.Chdir(dir)
	mustRun(t, "add", "from cwd")
	if out := mustRun(t, "--repo", dir, "ls"); out != "#1 [backlog] from cwd\n" {
		t.Errorf("ls = %q", out)
	}
	// A relative --repo is resolved against the working directory.
	if out := mustRun(t, "--repo", ".", "ls"); out != "#1 [backlog] from cwd\n" {
		t.Errorf("ls --repo . = %q", out)
	}
}

func TestMvWithFrom(t *testing.T) {
	dir := setup(t)
	mustRun(t, "--repo", dir, "add", "claim me", "-s", "todo")
	if out := mustRun(t, "--repo", dir, "mv", "1", "doing", "--from", "todo"); out != "#1 [doing] claim me\n" {
		t.Errorf("mv output = %q", out)
	}
	_, err := run(t, "--repo", dir, "mv", "1", "done", "--from", "todo")
	if err == nil || !strings.Contains(err.Error(), "doing") {
		t.Errorf("conflict error = %v, want mention of current status", err)
	}
	if out := mustRun(t, "--repo", dir, "ls"); out != "#1 [doing] claim me\n" {
		t.Errorf("task changed after conflict: %q", out)
	}
}

func TestEditChangesOnlyGivenFields(t *testing.T) {
	dir := setup(t)
	mustRun(t, "--repo", dir, "add", "old title", "-s", "todo")
	if out := mustRun(t, "--repo", dir, "edit", "1", "-t", "new title"); out != "#1 [todo] new title\n" {
		t.Errorf("edit output = %q", out)
	}
	if _, err := run(t, "--repo", dir, "edit", "1"); err == nil {
		t.Error("edit without flags accepted")
	}
}

func TestRmAndBadIDs(t *testing.T) {
	dir := setup(t)
	mustRun(t, "--repo", dir, "add", "remove me")
	if out := mustRun(t, "--repo", dir, "rm", "1"); out != "deleted #1\n" {
		t.Errorf("rm output = %q", out)
	}
	if out := mustRun(t, "--repo", dir, "ls"); out != "" {
		t.Errorf("ls after rm = %q", out)
	}
	for _, args := range [][]string{{"rm", "1"}, {"rm", "abc"}, {"mv", "0", "done"}, {"edit", "-3", "-t", "x"}} {
		if _, err := run(t, append([]string{"--repo", dir}, args...)...); err == nil {
			t.Errorf("%v returned no error", args)
		}
	}
}

func TestMvIsRepoScoped(t *testing.T) {
	a := setup(t)
	b := newDir(t)
	mustRun(t, "--repo", a, "add", "in a")
	if _, err := run(t, "--repo", b, "mv", "1", "done"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("cross-repo mv error = %v, want not found", err)
	}
}
