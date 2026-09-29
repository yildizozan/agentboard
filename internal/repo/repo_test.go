package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// isolateGit keeps the developer's git config out of the tests.
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newRepo creates a git repo with one commit and returns its real (symlink-free) path.
func newRepo(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func mustResolve(t *testing.T, dir string) string {
	t.Helper()
	key, err := Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", dir, err)
	}
	return key
}

func TestResolveRootAndSubdir(t *testing.T) {
	isolateGit(t)
	root := newRepo(t, t.TempDir(), "proj")
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := mustResolve(t, root); got != root {
		t.Errorf("Resolve(root) = %q, want %q", got, root)
	}
	if got := mustResolve(t, sub); got != root {
		t.Errorf("Resolve(subdir) = %q, want %q", got, root)
	}
}

func TestResolveWorktreeSharesMainRepo(t *testing.T) {
	isolateGit(t)
	base := t.TempDir()
	root := newRepo(t, base, "proj")
	wt := filepath.Join(base, "proj-feature")
	git(t, root, "worktree", "add", "-q", "-b", "feature", wt)

	if got := mustResolve(t, wt); got != root {
		t.Errorf("Resolve(worktree) = %q, want main repo %q", got, root)
	}
}

func TestResolveSubmodulesAreDistinct(t *testing.T) {
	isolateGit(t)
	base := t.TempDir()
	lib := newRepo(t, base, "lib")
	super := newRepo(t, base, "super")
	git(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "a")
	git(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "b")

	a := mustResolve(t, filepath.Join(super, "a"))
	b := mustResolve(t, filepath.Join(super, "b"))
	if a == b || a == super || b == super {
		t.Errorf("submodule keys must differ from each other and the superproject: a=%q b=%q super=%q", a, b, super)
	}
}

func TestResolveNonGitDir(t *testing.T) {
	isolateGit(t)
	dir := filepath.Join(t.TempDir(), "plain")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if got := mustResolve(t, dir); got != want {
		t.Errorf("Resolve(non-git) = %q, want %q", got, want)
	}
}

func TestResolveFollowsSymlinks(t *testing.T) {
	isolateGit(t)
	base := t.TempDir()
	root := newRepo(t, base, "proj")
	link := filepath.Join(base, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if got := mustResolve(t, link); got != root {
		t.Errorf("Resolve(symlink) = %q, want %q", got, root)
	}
}

func TestResolveRejectsBadInput(t *testing.T) {
	isolateGit(t)
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"", "relative/path", filepath.Join(t.TempDir(), "missing"), file} {
		if _, err := Resolve(dir); err == nil {
			t.Errorf("Resolve(%q) returned no error", dir)
		}
	}
}
