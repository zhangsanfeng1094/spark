package app

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"spark/internal/config"
	"spark/internal/tui"
)

func newMcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage MCP servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			return manageMcpServers()
		},
	}

	cmd.AddCommand(newMcpListCmd())
	cmd.AddCommand(newMcpShowCmd())
	cmd.AddCommand(newMcpAddCmd())
	cmd.AddCommand(newMcpRemoveCmd())
	cmd.AddCommand(newMcpEnableCmd())
	cmd.AddCommand(newMcpDisableCmd())
	cmd.AddCommand(newMcpImportCmd())
	cmd.AddCommand(newMcpSyncCmd())
	cmd.AddCommand(newMcpExportCmd())
	return cmd
}

func newMcpListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List MCP servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if len(cfg.McpServers) == 0 {
				fmt.Println("No MCP servers configured.")
				return nil
			}
			fmt.Println(describeMcpServers("MCP servers", cfg.McpServers))
			return nil
		},
	}
}

func newMcpShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show MCP server details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			server := cfg.GetMcpServer(args[0])
			if server == nil {
				return fmt.Errorf("MCP server not found: %s", args[0])
			}
			fmt.Printf("name: %s\nenabled: %t\n", config.McpServerName(args[0]), server.Enabled)
			if server.Command != "" {
				fmt.Printf("command: %s\n", server.Command)
			}
			if len(server.Args) > 0 {
				fmt.Printf("args: %s\n", strings.Join(server.Args, " "))
			}
			if server.URL != "" {
				fmt.Printf("url: %s\n", server.URL)
			}
			if len(server.Env) > 0 {
				for key, value := range server.Env {
					fmt.Printf("env.%s=%s\n", key, value)
				}
			}
			return nil
		},
	}
}

func newMcpAddCmd() *cobra.Command {
	var command string
	var url string
	var argsCSV string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add an MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			name := config.McpServerName(args[0])
			server, err := buildMcpServerFromFlagsOrPrompt(command, url, argsCSV)
			if err != nil {
				return err
			}
			cfg.SetMcpServer(name, server)
			return config.Save(cfg)
		},
	}
	cmd.Flags().StringVar(&command, "command", "", "Command for stdio transport")
	cmd.Flags().StringVar(&url, "url", "", "URL for HTTP transport")
	cmd.Flags().StringVar(&argsCSV, "args", "", "Comma-separated args for stdio transport")
	return cmd
}

func newMcpRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if !cfg.RemoveMcpServer(args[0]) {
				return fmt.Errorf("MCP server not found: %s", args[0])
			}
			return config.Save(cfg)
		},
	}
}

func newMcpEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <name>",
		Short: "Enable an MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.EnableMcpServer(args[0]); err != nil {
				return err
			}
			return config.Save(cfg)
		},
	}
}

func newMcpDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <name>",
		Short: "Disable an MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.DisableMcpServer(args[0], "disabled by spark"); err != nil {
				return err
			}
			return config.Save(cfg)
		},
	}
}

func newMcpImportCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "import <source>",
		Short: "Import MCP servers from Codex or Claude",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			peer := canonicalMcpTransferPeer(args[0])
			if peer == "" {
				return fmt.Errorf("unsupported import source: %s", args[0])
			}
			msg, err := importMcpFromPeer(peer, dryRun)
			if err != nil {
				return err
			}
			fmt.Println(msg)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview import without saving")
	return cmd
}

func newMcpSyncCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "sync <target>",
		Short: "Sync MCP servers to Codex or Claude",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			peer := canonicalMcpTransferPeer(args[0])
			if peer == "" {
				return fmt.Errorf("unsupported sync target: %s", args[0])
			}
			msg, err := exportMcpToPeer(peer, dryRun)
			if err != nil {
				return err
			}
			fmt.Println(msg)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview sync without saving")
	return cmd
}

func newMcpExportCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "export <target>",
		Short: "Export MCP servers to Codex or Claude",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			peer := canonicalMcpTransferPeer(args[0])
			if peer == "" {
				return fmt.Errorf("unsupported export target: %s", args[0])
			}
			msg, err := exportMcpToPeer(peer, dryRun)
			if err != nil {
				return err
			}
			fmt.Println(msg)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview sync without saving")
	return cmd
}

func buildMcpServerFromFlagsOrPrompt(command, url, argsCSV string) (*config.McpServerConfig, error) {
	command = strings.TrimSpace(command)
	url = strings.TrimSpace(url)
	if command == "" && url == "" {
		choice, err := tui.SelectOne("Select transport:", []string{"stdio", "http"})
		if err != nil {
			return nil, err
		}
		switch choice {
		case "stdio":
			command, err = tui.InputWithDefault("Command", "")
			if err != nil {
				return nil, err
			}
			args, err := tui.InputCSV("Args", nil)
			if err != nil {
				return nil, err
			}
			return config.NewStdioMcpServer(command, args), nil
		case "http":
			url, err = tui.InputWithDefault("URL", "")
			if err != nil {
				return nil, err
			}
			return config.NewHttpMcpServer(url), nil
		}
	}
	if command != "" {
		var args []string
		if strings.TrimSpace(argsCSV) != "" {
			for _, part := range strings.Split(argsCSV, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					args = append(args, part)
				}
			}
		}
		return config.NewStdioMcpServer(command, args), nil
	}
	if url != "" {
		return config.NewHttpMcpServer(url), nil
	}
	return nil, fmt.Errorf("either command or url is required")
}

func manageMcpServers() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return tui.ManageMCPDashboard(cfg)
}
