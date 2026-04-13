package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCodexMcpServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
model = "gpt-5"

[mcp_servers.docs]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-docs"]
enabled = true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	servers, err := LoadCodexMcpServers(path)
	if err != nil {
		t.Fatalf("LoadCodexMcpServers failed: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers["docs"] == nil || servers["docs"].Command != "npx" {
		t.Fatalf("unexpected docs server: %#v", servers["docs"])
	}
}

func TestLoadCodexMcpServersParsesMultipleServerTables(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[mcp_servers.demo]
command = "echo"
args = ["hello"]

[mcp_servers.docs]
url = "https://example.com/mcp"
enabled = true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	servers, err := LoadCodexMcpServers(path)
	if err != nil {
		t.Fatalf("LoadCodexMcpServers failed: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d (%#v)", len(servers), servers)
	}
	if servers["demo"] == nil || servers["demo"].Command != "echo" {
		t.Fatalf("unexpected demo server: %#v", servers["demo"])
	}
	if servers["docs"] == nil || servers["docs"].URL != "https://example.com/mcp" {
		t.Fatalf("unexpected docs server: %#v", servers["docs"])
	}
}

func TestLoadCodexMcpServersDefaultsMissingEnabledToTrue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[mcp_servers.demo]
command = "echo"
args = ["hello"]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	servers, err := LoadCodexMcpServers(path)
	if err != nil {
		t.Fatalf("LoadCodexMcpServers failed: %v", err)
	}
	if servers["demo"] == nil {
		t.Fatalf("expected demo server, got %#v", servers)
	}
	if !servers["demo"].Enabled {
		t.Fatalf("expected missing enabled to default true, got %#v", servers["demo"])
	}
}

func TestLoadCodexMcpServersParsesNestedToolConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[mcp_servers.augment-context-engine]
command = "auggie"
args = ["--mcp"]

[mcp_servers.augment-context-engine.tools.codebase-retrieval]
approval_mode = "approve"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	servers, err := LoadCodexMcpServers(path)
	if err != nil {
		t.Fatalf("LoadCodexMcpServers failed: %v", err)
	}
	server := servers["augment-context-engine"]
	if server == nil {
		t.Fatalf("expected augment-context-engine server, got %#v", servers)
	}
	rawTool, exists := server.Tools["codebase-retrieval"]
	if !exists {
		t.Fatalf("expected nested tool config, got %#v", server.Tools)
	}
	tool, ok := any(rawTool).(map[string]any)
	if !ok {
		t.Fatalf("expected nested tool config map, got %#v", rawTool)
	}
	if tool["approval_mode"] != "approve" {
		t.Fatalf("expected approval_mode=approve, got %#v", tool)
	}
}

func TestSaveCodexMcpServersPreservesOtherConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
model = "gpt-5"
approval_policy = "never"

[model_providers.spark]
name = "Spark"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	err := SaveCodexMcpServers(path, map[string]*McpServerConfig{
		"docs": {
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-docs"},
			Enabled: true,
		},
	})
	if err != nil {
		t.Fatalf("SaveCodexMcpServers failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `model = "gpt-5"`) {
		t.Fatalf("expected model to be preserved, got:\n%s", text)
	}
	if !strings.Contains(text, `[mcp_servers.docs]`) {
		t.Fatalf("expected mcp server section, got:\n%s", text)
	}
}

func TestSaveCodexMcpServersSkipsDisabledServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	err := SaveCodexMcpServers(path, map[string]*McpServerConfig{
		"docs": {
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-docs"},
			Enabled: true,
		},
		"github": {
			URL:     "https://example.com/mcp",
			Enabled: false,
		},
	})
	if err != nil {
		t.Fatalf("SaveCodexMcpServers failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `[mcp_servers.docs]`) {
		t.Fatalf("expected enabled server section, got:\n%s", text)
	}
	if strings.Contains(text, `[mcp_servers.github]`) {
		t.Fatalf("expected disabled server to be skipped, got:\n%s", text)
	}
}
