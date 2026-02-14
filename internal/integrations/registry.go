package integrations

import (
	"fmt"
	"sort"
	"strings"
)

var registry = map[string]Runner{
	"claude":   &Claude{},
	"codex":    &Codex{},
	"droid":    &Droid{},
	"opencode": &OpenCode{},
	"openclaw": &Openclaw{},
	"clawdbot": &Openclaw{},
	"moltbot":  &Openclaw{},
	"pi":       &Pi{},
}

func Get(name string) (Runner, bool) {
	r, ok := registry[strings.ToLower(name)]
	return r, ok
}

func Names() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(registry))
	for n := range registry {
		if n == "clawdbot" || n == "moltbot" {
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

func Must(name string) Runner {
	r, ok := Get(name)
	if !ok {
		panic(fmt.Sprintf("unknown integration: %s", name))
	}
	return r
}
