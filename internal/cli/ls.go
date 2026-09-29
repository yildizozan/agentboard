package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yildizozan/agentboard/internal/store"
	"github.com/yildizozan/agentboard/internal/task"
)

func newLsCmd(opts *options) *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List the repository's tasks (done tasks only with -s done)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			statuses := task.ActiveStatuses()
			if status != "" {
				st, err := task.ParseStatus(status)
				if err != nil {
					return err
				}
				statuses = []task.Status{st}
			}
			return opts.withBoard(func(s *store.Store, repoKey string) error {
				tasks, err := s.List(cmd.Context(), repoKey, statuses)
				if err != nil {
					return err
				}
				for _, tk := range tasks {
					fmt.Fprintln(cmd.OutOrStdout(), tk)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVarP(&status, "status", "s", "", "only show this status (default: all except done)")
	return cmd
}
