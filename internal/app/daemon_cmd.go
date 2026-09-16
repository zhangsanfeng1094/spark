package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"spark/internal/compat/daemon"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the shared Spark background proxy daemon",
	}

	cmd.AddCommand(newDaemonRunCmd())
	cmd.AddCommand(newDaemonStartCmd())
	cmd.AddCommand(newDaemonStopCmd())
	cmd.AddCommand(newDaemonStatusCmd())
	return cmd
}

func newDaemonRunCmd() *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the shared daemon in foreground",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			server, err := daemon.NewServer(ctx, addr, func(format string, a ...any) {
				fmt.Fprintf(cmd.OutOrStdout(), "[daemon] "+format+"\n", a...)
			})
			if err != nil {
				return err
			}

			go func() {
				<-ctx.Done()
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer shutdownCancel()
				_ = server.Shutdown(shutdownCtx)
			}()

			fmt.Fprintf(cmd.OutOrStdout(), "Spark shared proxy daemon listening on http://%s\n", server.Addr())
			return server.Start()
		},
	}

	cmd.Flags().StringVar(&addr, "addr", daemon.DefaultDaemonAddr, "Listen address for the shared daemon")
	return cmd
}

func newDaemonStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the shared proxy daemon in background",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dInfo, err := daemon.EnsureDaemon(context.Background(), func(format string, a ...any) {
				fmt.Fprintf(cmd.OutOrStdout(), format+"\n", a...)
			})
			if err != nil {
				return fmt.Errorf("failed to start daemon: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Spark shared daemon is ready at %s (pid: %d)\n", dInfo.BaseURL, dInfo.PID)
			return nil
		},
	}
}

func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the shared proxy daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemon.StopDaemon(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Spark shared daemon stopped")
			return nil
		},
	}
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the status of the shared proxy daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info, isRunning, err := daemon.StatusDaemon()
			if err != nil {
				return err
			}
			if !isRunning || info == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Spark shared daemon is NOT running")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Spark shared daemon is RUNNING\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  PID:      %d\n", info.PID)
			fmt.Fprintf(cmd.OutOrStdout(), "  Base URL: %s\n", info.BaseURL)
			if hr, err := daemon.CheckHealth(info.BaseURL); err == nil && hr != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "  Version:  %s\n", hr.Version)
				if hr.ExePath != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  Binary:   %s\n", hr.ExePath)
				}
			}
			return nil
		},
	}
}
