package integrations

import (
	"fmt"
	"sort"
	"strings"
)

var registry = map[string]Runner{
	"claude":     &Claude{},
	"codex":      &Codex{},
	"droid":      &Droid{},
	"grok":       &Grok{},
	"grok-build": &Grok{},
	"opencode":   &OpenCode{},
	"openclaw":   &Openclaw{},
	"clawdbot":   &Openclaw{},
	"moltbot":    &Openclaw{},
	"pi":         &Pi{},
}

func Get(name string) (Runner, bool) {
	r, ok := registry[strings.ToLower(name)]
	return r, ok
}

func Names() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(registry))
	for n := range registry {
		if n == "clawdbot" || n == "moltbot" || n == "grok-build" {
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
