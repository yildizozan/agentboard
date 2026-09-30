package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/yildizozan/agentboard/internal/store"
	"github.com/yildizozan/agentboard/internal/task"
)

func newMvCmd(opts *options) *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "mv <id> <status>",
		Short: "Move a task to another status",
		Long:  "Move a task to another status. With --from the move only happens if the task is still in that status.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := task.ParseStatus(args[1])
			if err != nil {
				return err
			}
			p := task.Patch{Status: &st}
			if from != "" {
				f, err := task.ParseStatus(from)
				if err != nil {
					return err
				}
				p.From = &f
			}
			return opts.updateTask(cmd, args[0], p)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "only move if the task is currently in this status")
	return cmd
}

func newEditCmd(opts *options) *cobra.Command {
	var priority string
	var epic int64
	cmd := &cobra.Command{
		Use:   "edit <id> [<body>]",
		Short: "Change a task's body, priority or epic",
		Long:  bodyHelp("Change a task's body, priority or epic; give only what changes. --epic moves a task to another epic; the parent cannot be removed."),
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var p task.Patch
			if len(args) == 2 {
				body, err := readBody(cmd, args[1])
				if err != nil {
					return err
				}
				p.Body = &body
			}
			if cmd.Flags().Changed("priority") {
				pr := task.Priority(priority)
				p.Priority = &pr
			}
			if cmd.Flags().Changed("epic") {
				p.Epic = &epic
			}
			return opts.updateTask(cmd, args[0], p)
		},
	}
	cmd.Flags().StringVarP(&priority, "priority", "p", "", "new priority: low, normal or high")
	cmd.Flags().Int64VarP(&epic, "epic", "e", 0, "positive id of the epic to move the task to")
	return cmd
}

func newShowCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Print a task's status line and Markdown body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			return opts.withBoard(func(s *store.Store, repoKey string) error {
				tk, err := s.Get(cmd.Context(), repoKey, id)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), tk.Detail())
				return nil
			})
		},
	}
}

func newRmCmd(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Delete a task permanently",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			return opts.withBoard(func(s *store.Store, repoKey string) error {
				if err := s.Delete(cmd.Context(), repoKey, id); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "deleted #%d\n", id)
				return nil
			})
		},
	}
}

// updateTask applies p to the task with the given id argument and prints the result.
func (o *options) updateTask(cmd *cobra.Command, idArg string, p task.Patch) error {
	id, err := parseID(idArg)
	if err != nil {
		return err
	}
	return o.withBoard(func(s *store.Store, repoKey string) error {
		tk, err := s.Update(cmd.Context(), repoKey, id, p)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), tk)
		return nil
	})
}

func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid task id %q: must be a positive number", s)
	}
	return id, nil
}
