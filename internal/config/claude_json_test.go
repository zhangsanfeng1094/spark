package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadClaudeUserMcpServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")
	content := `{
  "installMethod": "local",
  "mcpServers": {
    "deepwiki": {
      "type": "http",
      "url": "https://mcp.deepwiki.com/mcp"
    },
    "docs": {
      "type": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-docs"],
      "env": {
        "DOCS_TOKEN": "secret"
      }
    }
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	servers, err := LoadClaudeUserMcpServers(path)
	if err != nil {
		t.Fatalf("LoadClaudeUserMcpServers failed: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %#v", servers)
	}
	if servers["deepwiki"] == nil || servers["deepwiki"].URL != "https://mcp.deepwiki.com/mcp" {
		t.Fatalf("unexpected deepwiki server: %#v", servers["deepwiki"])
	}
	if !servers["deepwiki"].Enabled {
		t.Fatalf("expected imported claude servers to default enabled, got %#v", servers["deepwiki"])
	}
	if servers["docs"] == nil || servers["docs"].Command != "npx" {
		t.Fatalf("unexpected docs server: %#v", servers["docs"])
	}
	if got := servers["docs"].Env["DOCS_TOKEN"]; got != "secret" {
		t.Fatalf("expected env to round-trip, got %q", got)
	}
}

func TestSaveClaudeUserMcpServersPreservesOtherConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")
	content := `{
  "installMethod": "local",
  "projects": {
    "/tmp/demo": {
      "mcpServers": {
        "local-only": {
          "type": "http",
          "url": "https://example.com/local"
        }
      }
    }
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	err := SaveClaudeUserMcpServers(path, map[string]*McpServerConfig{
		"deepwiki": {
			URL:     "https://mcp.deepwiki.com/mcp",
			Enabled: true,
		},
		"docs": {
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-docs"},
			Env: map[string]string{
				"DOCS_TOKEN": "secret",
			},
			Enabled: true,
		},
		"disabled": {
			URL:     "https://example.com/disabled",
			Enabled: false,
		},
	})
	if err != nil {
		t.Fatalf("SaveClaudeUserMcpServers failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`"installMethod": "local"`,
		`"/tmp/demo"`,
		`"mcpServers": {`,
		`"deepwiki"`,
		`"type": "http"`,
		`"url": "https://mcp.deepwiki.com/mcp"`,
		`"docs"`,
		`"type": "stdio"`,
		`"command": "npx"`,
		`"DOCS_TOKEN": "secret"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected saved claude config to contain %q, got:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"disabled"`) {
		t.Fatalf("expected disabled server to be skipped, got:\n%s", text)
	}
	if !strings.Contains(text, `"local-only"`) {
		t.Fatalf("expected project-local config to be preserved, got:\n%s", text)
	}
}
