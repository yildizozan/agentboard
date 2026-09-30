package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/yildizozan/agentboard/internal/store"
	"github.com/yildizozan/agentboard/internal/task"
)

func newAddCmd(opts *options) *cobra.Command {
	var status, kind, priority string
	var epic int64
	cmd := &cobra.Command{
		Use:   "add <body>",
		Short: "Add a task to the repository's board",
		Long:  bodyHelp("Add a task to the repository's board."),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := readBody(cmd, args[0])
			if err != nil {
				return err
			}
			d := task.Draft{Body: body, Status: task.Status(status), Kind: task.Kind(kind), Priority: task.Priority(priority)}
			if epic != 0 {
				d.EpicID = &epic
			}
			return opts.withBoard(func(s *store.Store, repoKey string) error {
				tk, err := s.Add(cmd.Context(), repoKey, d)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), tk)
				return nil
			})
		},
	}
	cmd.Flags().StringVarP(&status, "status", "s", string(task.Backlog), "initial status: backlog, todo, doing or done")
	cmd.Flags().StringVarP(&kind, "kind", "k", string(task.TaskKind), "task or epic")
	cmd.Flags().StringVarP(&priority, "priority", "p", string(task.Normal), "low, normal or high")
	cmd.Flags().Int64VarP(&epic, "epic", "e", 0, "id of the epic this task belongs to")
	return cmd
}

// bodyHelp appends the card body rules shared by add and edit to summary.
func bodyHelp(summary string) string {
	return summary + ` The body is Markdown; its first line is the "# <title>" heading
(a plain first line becomes the heading). Pass - to read the body from stdin:

  agentboard add - <<'EOF'
  # Fix login bug

  ## Context
  Token expiry uses < instead of <=.
  EOF`
}

// readBody returns arg, or stdin when arg is "-".
func readBody(cmd *cobra.Command, arg string) (string, error) {
	if arg != "-" {
		return arg, nil
	}
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", fmt.Errorf("read body from stdin: %w", err)
	}
	return string(data), nil
}
