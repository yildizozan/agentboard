package cmd

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// run executes the root command with args and returns its combined output.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runContext(t, context.Background(), args...)
}

// runContext is run with a context, for commands that serve until it is canceled.
func runContext(t *testing.T, ctx context.Context, args ...string) (string, error) {
	t.Helper()
	skillContent = []byte("---\nname: agentboard\ndescription: Test skill\n---\n")
	var out bytes.Buffer
	cmd := newRootCmd("1.2.3")
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(ctx)
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

// setupBoard gives task fixtures a completed parent epic.
func setupBoard(t *testing.T) string {
	t.Helper()
	dir := setup(t)
	mustRun(t, "--repo", dir, "add", "# Test epic", "-k", "epic", "-s", "done", "-p", "low")
	return dir
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
	dir := setupBoard(t)
	out := mustRun(t, "--repo", dir, "add", "Fix login", "-e", "1")
	if out != "#2 [backlog] Fix login (epic #1)\n" {
		t.Errorf("add output = %q", out)
	}
}

func TestAddMarkdownBodyAndShow(t *testing.T) {
	dir := setupBoard(t)
	out := mustRun(t, "--repo", dir, "add", "# Write docs\n\n## Steps\n- [ ] README", "-s", "todo", "-e", "1")
	if out != "#2 [todo] Write docs (epic #1)\n" {
		t.Errorf("add output = %q", out)
	}
	if out := mustRun(t, "--repo", dir, "show", "2"); out != "#2 [todo] Write docs (epic #1)\nRevision: 1\n\n# Write docs\n\n## Steps\n- [ ] README\n" {
		t.Errorf("show output = %q", out)
	}
	if _, err := run(t, "--repo", dir, "show", "9"); err == nil {
		t.Error("show of missing task succeeded")
	}
}

func TestAddReadsBodyFromStdin(t *testing.T) {
	dir := setupBoard(t)
	skillContent = []byte("---\nname: agentboard\ndescription: Test skill\n---\n")
	var out bytes.Buffer
	cmd := newRootCmd("1.2.3")
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader("# From stdin\n\nbody text\n"))
	cmd.SetArgs([]string{"--repo", dir, "add", "-", "-e", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := mustRun(t, "--repo", dir, "show", "2"); got != "#2 [backlog] From stdin (epic #1)\nRevision: 1\n\n# From stdin\n\nbody text\n" {
		t.Errorf("show output = %q", got)
	}
}

func TestAddRejectsInvalidInput(t *testing.T) {
	dir := setup(t)
	if _, err := run(t, "--repo", dir, "add", "x", "-s", "later"); err == nil || !strings.Contains(err.Error(), "backlog, todo, doing, done") {
		t.Errorf("invalid status error = %v", err)
	}
	if _, err := run(t, "--repo", dir, "add", "   "); err == nil {
		t.Error("blank body accepted")
	}
	if _, err := run(t, "--repo", dir, "add", "## Context"); err == nil || !strings.Contains(err.Error(), "level-1 heading") {
		t.Errorf("deep heading error = %v", err)
	}
	if _, err := run(t, "--repo", dir, "add"); err == nil {
		t.Error("missing body accepted")
	}
}

func TestLsHidesDoneByDefault(t *testing.T) {
	dir := setupBoard(t)
	mustRun(t, "--repo", dir, "add", "open task", "-s", "doing", "-e", "1")
	mustRun(t, "--repo", dir, "add", "finished", "-s", "done", "-e", "1")

	if out := mustRun(t, "--repo", dir, "ls"); out != "#2 [doing] open task (epic #1)\n" {
		t.Errorf("ls = %q", out)
	}
	if out := mustRun(t, "--repo", dir, "ls", "-s", "done"); out != "#3 [done] finished (epic #1)\n#1 [done] [epic] [low] Test epic\n" {
		t.Errorf("ls -s done = %q", out)
	}
	if _, err := run(t, "--repo", dir, "ls", "-s", "later"); err == nil {
		t.Error("ls accepted invalid status")
	}
}

func TestLsIsolatesRepos(t *testing.T) {
	a := setupBoard(t)
	b := newDir(t)
	mustRun(t, "--repo", a, "add", "in a", "-e", "1")
	if out := mustRun(t, "--repo", b, "ls"); out != "" {
		t.Errorf("ls in other repo = %q, want empty", out)
	}
}

func TestRepoDefaultsToWorkingDirectory(t *testing.T) {
	dir := setupBoard(t)
	t.Chdir(dir)
	mustRun(t, "add", "from cwd", "-e", "1")
	if out := mustRun(t, "--repo", dir, "ls"); out != "#2 [backlog] from cwd (epic #1)\n" {
		t.Errorf("ls = %q", out)
	}
	// A relative --repo is resolved against the working directory.
	if out := mustRun(t, "--repo", ".", "ls"); out != "#2 [backlog] from cwd (epic #1)\n" {
		t.Errorf("ls --repo . = %q", out)
	}
}

func TestMvWithFrom(t *testing.T) {
	dir := setupBoard(t)
	mustRun(t, "--repo", dir, "add", "claim me", "-s", "todo", "-e", "1")
	if out := mustRun(t, "--repo", dir, "mv", "2", "doing", "--from", "todo"); out != "#2 [doing] claim me (epic #1)\n" {
		t.Errorf("mv output = %q", out)
	}
	_, err := run(t, "--repo", dir, "mv", "2", "done", "--from", "todo")
	if err == nil || !strings.Contains(err.Error(), "doing") {
		t.Errorf("conflict error = %v, want mention of current status", err)
	}
	if out := mustRun(t, "--repo", dir, "ls"); out != "#2 [doing] claim me (epic #1)\n" {
		t.Errorf("task changed after conflict: %q", out)
	}
}

func TestEditReplacesBody(t *testing.T) {
	dir := setupBoard(t)
	mustRun(t, "--repo", dir, "add", "old title", "-s", "todo", "-e", "1")
	if out := mustRun(t, "--repo", dir, "edit", "2", "# new title\n\nfindings"); out != "#2 [todo] new title (epic #1)\n" {
		t.Errorf("edit output = %q", out)
	}
	if _, err := run(t, "--repo", dir, "edit", "2"); err == nil {
		t.Error("edit without changes accepted")
	}
	if _, err := run(t, "--repo", dir, "edit", "2", " "); err == nil {
		t.Error("edit with blank body accepted")
	}
}

func TestRmAndBadIDs(t *testing.T) {
	dir := setupBoard(t)
	mustRun(t, "--repo", dir, "add", "remove me", "-e", "1")
	if out := mustRun(t, "--repo", dir, "rm", "2"); out != "deleted #2\n" {
		t.Errorf("rm output = %q", out)
	}
	if out := mustRun(t, "--repo", dir, "ls"); out != "" {
		t.Errorf("ls after rm = %q", out)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"rm", "2"}, "not found"},
		{[]string{"rm", "abc"}, "invalid task id"},
		{[]string{"mv", "0", "done"}, "invalid task id"},
		{[]string{"edit", "0", "# x"}, "invalid task id"},
		{[]string{"show", "0"}, "invalid task id"},
	} {
		if _, err := run(t, append([]string{"--repo", dir}, tc.args...)...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v error = %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestMergeFoldsSourceIntoTarget(t *testing.T) {
	dir := setupBoard(t)
	mustRun(t, "--repo", dir, "add", "# Fix login", "-s", "todo", "-e", "1")
	mustRun(t, "--repo", dir, "add", "# Login broken\n\nsame bug", "-e", "1")
	if out := mustRun(t, "--repo", dir, "merge", "2", "3"); out != "merged #3 into #2\n#2 [todo] Fix login (epic #1)\n" {
		t.Errorf("merge output = %q", out)
	}
	if out := mustRun(t, "--repo", dir, "show", "2"); out != "#2 [todo] Fix login (epic #1)\nRevision: 2\n\n# Fix login\n\n## Merged from #3: Login broken\n\nsame bug\n" {
		t.Errorf("show after merge = %q", out)
	}
	if out := mustRun(t, "--repo", dir, "ls"); out != "#2 [todo] Fix login (epic #1)\n" {
		t.Errorf("ls after merge = %q", out)
	}
}

func TestMergeReportsBadInput(t *testing.T) {
	dir := setupBoard(t)
	mustRun(t, "--repo", dir, "add", "# target", "-e", "1")
	mustRun(t, "--repo", dir, "add", "# claimed", "-s", "doing", "-e", "1")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"merge", "2", "3"}, "#3 is in doing"},
		{[]string{"merge", "2", "2"}, "into itself"},
		{[]string{"merge", "2", "9"}, "not found"},
		{[]string{"merge", "x", "3"}, "invalid task id"},
		{[]string{"merge", "2"}, "accepts 2 arg"},
	} {
		if _, err := run(t, append([]string{"--repo", dir}, tc.args...)...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v error = %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestEpicsAndPrioritiesFromTheCLI(t *testing.T) {
	dir := setup(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"add", "# Auth rewrite", "-k", "epic", "-p", "high"}, "#1 [backlog] [epic] [high] Auth rewrite\n"},
		{[]string{"add", "# Login", "-e", "1", "-p", "low"}, "#2 [backlog] [low] Login (epic #1)\n"},
		{[]string{"add", "# Unrelated", "-k", "epic", "-s", "todo"}, "#3 [todo] [epic] Unrelated\n"},
		{[]string{"ls", "-e", "1"}, "#2 [backlog] [low] Login (epic #1)\n"},
		{[]string{"edit", "2", "-p", "high"}, "#2 [backlog] [high] Login (epic #1)\n"},
		{[]string{"edit", "2", "# Login page", "-e", "3"}, "#2 [backlog] [high] Login page (epic #3)\n"},
	} {
		if out := mustRun(t, append([]string{"--repo", dir}, tc.args...)...); out != tc.want {
			t.Errorf("%v = %q, want %q", tc.args, out, tc.want)
		}
	}
}

func TestEpicAndPriorityErrorsFromTheCLI(t *testing.T) {
	dir := setupBoard(t)
	mustRun(t, "--repo", dir, "add", "# plain", "-e", "1")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"add", "# x", "-k", "story"}, "invalid kind"},
		{[]string{"add", "# x", "-p", "urgent"}, "invalid priority"},
		{[]string{"add", "# x", "-e", "9"}, "not found"},
		{[]string{"add", "# x", "-e", "2"}, "not an epic"},
		{[]string{"edit", "2"}, "nothing to change"},
		{[]string{"edit", "2", "-p", "urgent"}, "invalid priority"},
	} {
		if _, err := run(t, append([]string{"--repo", dir}, tc.args...)...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v error = %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestMvIsRepoScoped(t *testing.T) {
	a := setupBoard(t)
	b := newDir(t)
	mustRun(t, "--repo", a, "add", "in a", "-e", "1")
	if _, err := run(t, "--repo", b, "mv", "2", "done"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("cross-repo mv error = %v, want not found", err)
	}
}

func TestBoardRejectsNonLoopbackAddr(t *testing.T) {
	setup(t)
	// A canceled context makes a wrongly accepted address return at once instead of serving forever.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, addr := range []string{"0.0.0.0:0", ":0", "192.168.1.10:0", "example.com:0", "nonsense"} {
		if _, err := runContext(t, ctx, "board", "--addr", addr); err == nil || !strings.Contains(err.Error(), "--addr") {
			t.Errorf("board --addr %s error = %v, want an --addr error", addr, err)
		}
	}
	for _, addr := range []string{"127.0.0.1:0", "localhost:0", "[::1]:0"} {
		if err := checkLoopback(addr); err != nil {
			t.Errorf("checkLoopback(%s) = %v", addr, err)
		}
	}
}

func TestBoardServesUntilCanceled(t *testing.T) {
	dir := setupBoard(t)
	mustRun(t, "--repo", dir, "add", "# served card", "-e", "1")
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pr, pw := io.Pipe()
	cmd := newRootCmd("1.2.3")
	cmd.SetOut(pw)
	cmd.SetArgs([]string{"--repo", dir, "board", "--addr", "127.0.0.1:0"})
	done := make(chan error, 1)
	go func() {
		done <- cmd.ExecuteContext(ctx)
		pw.Close()
	}()

	line, err := bufio.NewReader(pr).ReadString('\n')
	if err != nil {
		t.Fatalf("read board URL: %v", err)
	}
	boardURL := strings.TrimSpace(strings.TrimPrefix(line, "agentboard board:"))
	u, err := url.Parse(boardURL)
	wantPath := resolved
	if !strings.HasPrefix(wantPath, "/") {
		wantPath = "/" + wantPath
	}
	if err != nil || u.Scheme != "http" || u.Host == "" || u.Path != wantPath {
		t.Fatalf("printed URL %q, want http://<addr> with decoded path %q", boardURL, wantPath)
	}

	res, err := http.Get("http://" + u.Host + "/api/tasks?repo=" + url.QueryEscape(resolved))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(body), `"title":"served card"`) {
		t.Errorf("GET tasks = %d %s", res.StatusCode, body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("board returned %v after cancel, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("board did not stop after its context was canceled")
	}
}

func TestBoardPathShowsTheRepoPath(t *testing.T) {
	for in, want := range map[string]string{
		"/Users/ozan.yildiz/projects/agent-todo": "/Users/ozan.yildiz/projects/agent-todo",
		"/work/a b#c?d%e":                        "/work/a%20b%23c%3Fd%25e",
		`C:\Users\x`:                             "/C:%5CUsers%5Cx",
	} {
		if got := boardPath(in); got != want {
			t.Errorf("boardPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBoardURLWithoutResolvableRepo(t *testing.T) {
	opts := &options{repoDir: filepath.Join(t.TempDir(), "missing")}
	if got := boardURL("127.0.0.1:7420", opts); got != "http://127.0.0.1:7420/" {
		t.Errorf("boardURL = %q, want the URL without a repo", got)
	}
}

func TestDataDirDefaultsToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homeEnv, "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := newDir(t)
	mustRun(t, "--repo", dir, "add", "# Test epic", "-k", "epic", "-s", "done", "-p", "low")
	mustRun(t, "--repo", dir, "add", "stored in home", "-e", "1")
	if _, err := os.Stat(filepath.Join(home, ".agentboard", "agentboard.db")); err != nil {
		t.Errorf("database not in ~/.agentboard: %v", err)
	}
}

func TestVersionFlag(t *testing.T) {
	if out := mustRun(t, "--version"); !strings.Contains(out, "1.2.3") {
		t.Errorf("--version = %q", out)
	}
}

func TestTaskRequiresEpicFromCLI(t *testing.T) {
	dir := setup(t)
	if _, err := run(t, "--repo", dir, "add", "# Missing epic"); err == nil || !strings.Contains(err.Error(), "epic") {
		t.Fatalf("missing epic error=%v", err)
	}
	mustRun(t, "--repo", dir, "add", "# Epic", "-k", "epic")
	mustRun(t, "--repo", dir, "add", "# Child", "-e", "1")
	for _, args := range [][]string{{"edit", "2", "-e", "0"}, {"rm", "1"}} {
		if _, err := run(t, append([]string{"--repo", dir}, args...)...); err == nil {
			t.Fatalf("%v accepted", args)
		}
	}
	mustRun(t, "--repo", dir, "rm", "2")
	mustRun(t, "--repo", dir, "rm", "1")
}

func TestEditAndMoveCheckExpectedRevision(t *testing.T) {
	dir := setupBoard(t)
	mustRun(t, "--repo", dir, "add", "# Original", "-e", "1")
	mustRun(t, "--repo", dir, "edit", "2", "# Agent update", "--expected-revision", "1")
	for _, args := range [][]string{
		{"edit", "2", "# Stale draft", "--expected-revision", "1"},
		{"mv", "2", "doing", "--expected-revision", "1"},
	} {
		if _, err := run(t, append([]string{"--repo", dir}, args...)...); err == nil || !strings.Contains(err.Error(), "revision") {
			t.Errorf("%v error = %v, want revision conflict", args, err)
		}
	}
	if out := mustRun(t, "--repo", dir, "show", "2"); !strings.Contains(out, "Revision: 2") || !strings.Contains(out, "# Agent update") || !strings.Contains(out, "[backlog]") {
		t.Fatalf("conflicting writes changed task: %q", out)
	}
	mustRun(t, "--repo", dir, "mv", "2", "doing", "--expected-revision", "2")
	if out := mustRun(t, "--repo", dir, "show", "2"); !strings.Contains(out, "Revision: 3") || !strings.Contains(out, "[doing]") {
		t.Fatalf("matching revision did not move task: %q", out)
	}
	if _, err := run(t, "--repo", dir, "edit", "2", "# Invalid", "--expected-revision", "0"); err == nil {
		t.Fatal("zero expected revision accepted")
	}
}
