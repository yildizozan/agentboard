// Package repo maps a working directory to the key that identifies its board.
package repo

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Resolve returns the board key for dir, which must be an absolute path to an existing directory.
//
// Inside a git repository the key is derived from the common git dir, so subdirectories
// and linked worktrees share one board: for a normal repo it is the directory holding
// ".git"; for submodules and bare repos it is the git dir itself. Outside git the key is
// dir. Symlinks are resolved so the same directory always yields the same key.
func Resolve(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("path %q is not absolute", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path %q is not a directory", dir)
	}

	commonDir, err := gitCommonDir(dir)
	if err != nil {
		return "", err
	}
	key := dir
	switch {
	case commonDir == "":
		// not a git repository
	case filepath.Base(commonDir) == ".git":
		key = filepath.Dir(commonDir)
	default:
		key = commonDir
	}
	return filepath.EvalSymlinks(key)
}

// gitCommonDir returns the absolute common git dir for dir, or "" when dir is not in a repository.
func gitCommonDir(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return strings.TrimSpace(string(out)), nil
	case errors.As(err, &exitErr):
		return "", nil
	default:
		// git missing or not runnable: failing is safer than splitting one repo into several boards.
		return "", fmt.Errorf("run git: %w", err)
	}
}
