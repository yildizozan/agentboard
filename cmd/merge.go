package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yildizozan/agentboard/internal/store"
	"github.com/yildizozan/agentboard/internal/task"
)

func newMergeCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "merge <target> <source>",
		Short: "Fold one task into another and delete it",
		Long: "Merge <source> into <target>: the source's body is appended to the target as a\n" +
			"\"## Merged from #<source>\" section and the source is deleted. The target keeps its\n" +
			"title and status. A source in doing is refused, since an agent may be working on it.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetID, err := parseID(args[0])
			if err != nil {
				return err
			}
			sourceID, err := parseID(args[1])
			if err != nil {
				return err
			}
			return opts.withBoard(func(s *store.Store, repoKey string) error {
				tk, err := s.Merge(cmd.Context(), repoKey, targetID, sourceID)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), task.MergedLine(sourceID, tk))
				return nil
			})
		},
	}
}
