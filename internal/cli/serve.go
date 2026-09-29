package cli

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/yildizozan/agentboard/internal/mcpserver"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP server over stdio",
		Long:  "Run the MCP server over stdio. Agents pass their working directory with each call, so one server serves every repository.\nStdout carries the MCP protocol only; diagnostics go to stderr.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return mcpserver.New(s).Run(ctx, &mcp.StdioTransport{})
		},
	}
}
