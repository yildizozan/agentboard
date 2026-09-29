// Package cli wires the cobra commands to the store and repo resolution.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

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
