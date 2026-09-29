package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yildizozan/agentboard/internal/store"
	"github.com/yildizozan/agentboard/internal/task"
)

func newAddCmd(opts *options) *cobra.Command {
	var description, status string
	cmd := &cobra.Command{
		Use:   "add <title>",
		Short: "Add a task to the repository's board",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := task.ParseStatus(status)
			if err != nil {
				return err
			}
			return opts.withBoard(func(s *store.Store, repoKey string) error {
				tk, err := s.Add(cmd.Context(), repoKey, args[0], description, st)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), tk)
				return nil
			})
		},
	}
	cmd.Flags().StringVarP(&description, "description", "d", "", "task description")
	cmd.Flags().StringVarP(&status, "status", "s", string(task.Backlog), "initial status: backlog, todo, doing or done")
	return cmd
}
