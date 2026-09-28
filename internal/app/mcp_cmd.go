package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"spark/internal/config"
	"spark/internal/mcp"
	"spark/internal/tui"
)

func newMcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage MCP servers (per-agent bindings)",
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
	cmd.AddCommand(newMcpTestCmd())
	cmd.AddCommand(newMcpImportCmd())
	cmd.AddCommand(newMcpSyncCmd())
	cmd.AddCommand(newMcpExportCmd())
	return cmd
}

func newMcpListCmd() *cobra.Command {
	var agent string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List MCP servers and their per-agent bindings",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if len(cfg.McpServers) == 0 {
				fmt.Println("No MCP servers configured.")
				return nil
			}
			if agent != "" {
				fmt.Println(describeMcpServersForAgent("MCP servers enabled for "+agent, cfg, agent))
				return nil
			}
			fmt.Println(describeMcpServers("MCP servers", cfg.McpServers))
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "Only show servers enabled for this agent (codex|claude|one|grok|agy)")
	return cmd
}

func newMcpShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show MCP server definition and agent bindings",
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
			fmt.Printf("name: %s\n", config.McpServerName(args[0]))
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
			fmt.Println("agents:")
			for _, agent := range config.McpAgents() {
				enabled := cfg.McpAgentEnabled(args[0], agent)
				state := "off"
				if enabled {
					state = "on"
				}
				overrides := ""
				if binding := cfg.McpBinding(args[0], agent); binding != nil {
					var parts []string
					if binding.Command != "" {
						parts = append(parts, "command="+binding.Command)
					}
					if binding.URL != "" {
						parts = append(parts, "url="+binding.URL)
					}
					if len(parts) > 0 {
						overrides = " (" + strings.Join(parts, ", ") + ")"
					}
				}
				fmt.Printf("  %-8s %s%s\n", agent, state, overrides)
			}
			return nil
		},
	}
}

func newMcpAddCmd() *cobra.Command {
	var command string
	var url string
	var argsCSV string
	var agents string
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
			if agents != "" {
				// Explicit --agent list: disable definition default, enable
				// only the listed agents.
				server.Enabled = false
				for _, agent := range strings.Split(agents, ",") {
					agent = config.McpCanonicalAgent(agent)
					if agent == "" {
						continue
					}
					cfg.ToggleMcpAgent(name, agent, true)
				}
			}
			cfg.SetMcpServer(name, server)
			return config.Save(cfg)
		},
	}
	cmd.Flags().StringVar(&command, "command", "", "Command for stdio transport")
	cmd.Flags().StringVar(&url, "url", "", "URL for HTTP transport")
	cmd.Flags().StringVar(&argsCSV, "args", "", "Comma-separated args for stdio transport")
	cmd.Flags().StringVar(&agents, "agents", "", "Comma-separated agents to enable (codex,claude,one,grok,agy); default: enabled for all agents")
	return cmd
}

func newMcpRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an MCP server (definition + all bindings)",
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
	var agent string
	cmd := &cobra.Command{
		Use:   "enable <name>",
		Short: "Enable an MCP server (definition default or one agent binding)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if agent != "" {
				if cfg.GetMcpServer(args[0]) == nil {
					return fmt.Errorf("MCP server not found: %s", args[0])
				}
				cfg.ToggleMcpAgent(args[0], agent, true)
			} else if err := cfg.EnableMcpServer(args[0]); err != nil {
				return err
			}
			return config.Save(cfg)
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "Enable only for this agent (codex|claude|one|grok|agy)")
	return cmd
}

func newMcpDisableCmd() *cobra.Command {
	var agent string
	cmd := &cobra.Command{
		Use:   "disable <name>",
		Short: "Disable an MCP server (definition default or one agent binding)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if agent != "" {
				if cfg.GetMcpServer(args[0]) == nil {
					return fmt.Errorf("MCP server not found: %s", args[0])
				}
				cfg.ToggleMcpAgent(args[0], agent, false)
			} else if err := cfg.DisableMcpServer(args[0], "disabled by spark"); err != nil {
				return err
			}
			return config.Save(cfg)
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "Disable only for this agent (codex|claude|one|grok|agy)")
	return cmd
}

// newMcpTestCmd implements `spark mcp test <name> [--agent]`.
//
// The test itself is server-level: spawn/initialize/tools-list against the
// shared definition (with binding overrides applied when --agent is given).
// It does not validate the agent's own runtime wiring, so the flag only
// changes which effective config is tested, never the test mechanics.
func newMcpTestCmd() *cobra.Command {
	var agent string
	cmd := &cobra.Command{
		Use:   "test <name>",
		Short: "Test an MCP server (server-level initialize + tools/list)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			name := config.McpServerName(args[0])
			server := cfg.GetMcpServer(name)
			if server == nil {
				return fmt.Errorf("MCP server not found: %s", name)
			}
			target := server
			scope := "server"
			if agent != "" {
				key := config.McpCanonicalAgent(agent)
				if key == "" || !isKnownMcpAgent(key) {
					return fmt.Errorf("unsupported agent: %s (expected codex|claude|one|grok|agy)", agent)
				}
				eff := cfg.McpEffectiveServer(name, key)
				if eff == nil {
					return fmt.Errorf("MCP server not found: %s", name)
				}
				target = eff
				scope = key + " binding (effective config)"
			}
			fmt.Printf("Testing %s (%s)...\n", name, scope)
			result := mcp.Test(target)
			if result.Err != "" {
				status := mcp.Summarize(target, result)
				fmt.Printf("✕ %s — %s failed\n", status.Headline, result.Stage)
				fmt.Println("  " + result.Err)
				for _, s := range status.Suggestions {
					fmt.Println("  Tip: " + s)
				}
				return fmt.Errorf("mcp test failed: %s", status.Headline)
			}
			fmt.Printf("✓ OK · %d tool(s) · %s\n", result.ToolsCount, result.Latency)
			if len(result.ToolNames) > 0 {
				fmt.Println("  tools: " + strings.Join(result.ToolNames, ", "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "Test the effective config for this agent binding (codex|claude|one|grok|agy)")
	return cmd
}

func isKnownMcpAgent(agent string) bool {
	for _, a := range config.McpAgents() {
		if a == agent {
			return true
		}
	}
	return false
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

// describeMcpServersForAgent lists servers enabled for one agent.
func describeMcpServersForAgent(title string, cfg *config.RootConfig, agent string) string {
	servers := cfg.McpServersForAgent(config.McpCanonicalAgent(agent))
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	lines := []string{fmt.Sprintf("%s (%d):", title, len(names))}
	for _, name := range names {
		server := servers[name]
		transport := strings.TrimSpace(server.Command)
		if transport == "" {
			transport = strings.TrimSpace(server.URL)
		}
		lines = append(lines, fmt.Sprintf("- %s [%s] %s", name, "enabled", transport))
	}
	return strings.Join(lines, "\n")
}
