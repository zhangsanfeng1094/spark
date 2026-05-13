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

func TestRenderDashboardSnapshotClampsCursorAndStripsANSI(t *testing.T) {
	view, err := RenderDashboardSnapshot("Spark", []DashboardAction{
		{Title: "Launch integration", Description: "Start Spark."},
		{Title: "Quit", Description: "Exit Spark."},
	}, DashboardSummary{CurrentProfile: "ollama", ConfigPath: "/tmp/config.json"}, 40, 10, 99)
	if err != nil {
		t.Fatalf("RenderDashboardSnapshot failed: %v", err)
	}

	plain := StripANSI(view)
	for _, want := range []string{"> Quit", "Exit Spark.", "Current profile: ollama"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected snapshot to contain %q, got %q", want, plain)
		}
	}
}
