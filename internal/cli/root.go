// Package cli wires the cobra commands to the store and repo resolution.
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/yildizozan/agentboard/internal/repo"
	"github.com/yildizozan/agentboard/internal/store"
)

// homeEnv overrides the data directory (default ~/.agentboard).
const homeEnv = "AGENTBOARD_HOME"

// options holds values shared by all subcommands.
type options struct {
	repoDir string
}

func newRootCmd() *cobra.Command {
	var opts options
	cmd := &cobra.Command{
		Use:           "agentboard",
		Short:         "Repo-scoped task board for coding agents",
		Long:          "agentboard keeps a backlog/todo/doing/done board per repository.\nAgents use it over MCP; humans use the CLI or the web board.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.PersistentFlags().StringVar(&opts.repoDir, "repo", "", "directory used to resolve the repository (default: current directory)")
	cmd.AddCommand(newAddCmd(&opts), newLsCmd(&opts), newMvCmd(&opts), newEditCmd(&opts), newRmCmd(&opts))
	return cmd
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

// openStore opens the database in the data directory.
func openStore() (*store.Store, error) {
	dir := os.Getenv(homeEnv)
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("find home directory: %w", err)
		}
		dir = filepath.Join(home, ".agentboard")
	}
	return store.Open(filepath.Join(dir, "agentboard.db"))
}

// resolveRepo returns the board key for --repo, or for the working directory when it is unset.
func (o *options) resolveRepo() (string, error) {
	dir := o.repoDir
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return repo.Resolve(abs)
}

// withBoard opens the store and resolves the repo, then runs fn with both.
func (o *options) withBoard(fn func(s *store.Store, repoKey string) error) error {
	repoKey, err := o.resolveRepo()
	if err != nil {
		return err
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()
	return fn(s, repoKey)
}
