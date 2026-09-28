package integrations

import (
	"fmt"
	"sort"
	"strings"

	"spark/internal/config"
)

var registry = map[string]Runner{
	"agy":        &Agy{},
	"claude":     &Claude{},
	"codex":      &Codex{},
	"grok":       &Grok{},
	"grok-build": &Grok{},
	"one":        &One{},
	"opencode":   &OpenCode{},
}

// hiddenRegistryAliases are accepted by Get for compatibility but never listed in Names().
var hiddenRegistryAliases = map[string]struct{}{
	"grok-build": {},
}

func Get(name string) (Runner, bool) {
	r, ok := registry[strings.ToLower(name)]
	return r, ok
}

func Names() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(registry))
	for n := range registry {
		if _, hidden := hiddenRegistryAliases[n]; hidden {
			continue
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func Must(name string) (Runner, error) {
	return GetOrErr(name)
}

func GetOrErr(name string) (Runner, error) {
	r, ok := Get(name)
	if !ok {
		return nil, fmt.Errorf("unknown integration: %s", name)
	}
	return r, nil
}

// McpAgentKeyFor maps an integration name to its canonical MCP binding agent
// key. Agents that do not consume Spark MCP bindings (e.g. opencode in this
// phase) return "".
func McpAgentKeyFor(name string) string {
	key := config.McpCanonicalAgent(name)
	switch key {
	case "codex", "claude", "one", "grok", "agy":
		return key
	default:
		return ""
	}
}
