package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/yildizozan/agentboard/internal/httpapi"
	"github.com/yildizozan/agentboard/web"
)

func newBoardCmd(opts *options) *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "board",
		Short: "Serve the web board on a loopback address",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := checkLoopback(addr); err != nil {
				return err
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()

			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "agentboard board:", boardURL(ln.Addr().String(), opts))

			srv := &http.Server{Handler: httpapi.New(s, web.UI()), ReadHeaderTimeout: 5 * time.Second}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				srv.Shutdown(shutdownCtx)
			}()
			if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:7420", "listen address; must be a loopback address")
	return cmd
}

// checkLoopback refuses addresses reachable from other machines.
func checkLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid --addr %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("--addr %q is not a loopback address; the board has no authentication", addr)
}

// boardURL preselects the current repository when it can be resolved.
func boardURL(addr string, opts *options) string {
	u := "http://" + addr + "/"
	if repoKey, err := opts.resolveRepo(); err == nil {
		u += "?repo=" + url.QueryEscape(repoKey)
	}
	return u
}
