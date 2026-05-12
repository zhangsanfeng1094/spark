package tui

import (
	"strings"
	"testing"
)

func TestDashboardViewIncludesContextAndDescription(t *testing.T) {
	m := &dashboardModel{
		title: "Spark",
		actions: []DashboardAction{
			{Title: "Launch integration", Description: "Start Spark with the selected coding agent integration."},
			{Title: "Manage profiles", Description: "Edit provider profiles."},
		},
		summary: DashboardSummary{
			CurrentProfile: "gptload",
			ConfigPath:     "/tmp/config.json",
		},
		width:  100,
		height: 30,
	}

	view := m.View()
	for _, want := range []string{"Launch integration", "Current profile: gptload", "Config file: /tmp/config.json"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected dashboard view to contain %q, got %q", want, view)
		}
	}
}
