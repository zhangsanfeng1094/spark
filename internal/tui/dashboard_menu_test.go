package tui

import (
	"strings"
	"testing"
)

func TestDashboardViewIncludesContextAndDescription(t *testing.T) {
	m := &dashboardModel{
		title: "Spark",
		actions: []DashboardAction{
			{Title: "Quick launch", Description: "Start Spark with the selected coding agent integration."},
			{Title: "Manage profiles", Description: "Edit provider profiles."},
		},
		summary: DashboardSummary{
			QuickLaunchIntegration: "codex",
			DefaultProfile:         "gptload",
			DefaultModel:           "gpt-5",
			ConfigPath:             "/tmp/config.json",
		},
		width:  100,
		height: 30,
	}

	view := StripANSI(m.View())
	for _, want := range []string{"Quick launch", "Quick launch: codex", "Default profile: gptload", "Default model: gpt-5", "Config file: /tmp/config.json"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected dashboard view to contain %q, got %q", want, view)
		}
	}
}

func TestRenderDashboardSnapshotClampsCursorAndStripsANSI(t *testing.T) {
	view, err := RenderDashboardSnapshot("Spark", []DashboardAction{
		{Title: "Quick launch", Description: "Start Spark."},
		{Title: "Quit", Description: "Exit Spark."},
	}, DashboardSummary{QuickLaunchIntegration: "claude", DefaultProfile: "ollama", DefaultModel: "sonnet", ConfigPath: "/tmp/config.json"}, 40, 10, 99)
	if err != nil {
		t.Fatalf("RenderDashboardSnapshot failed: %v", err)
	}

	plain := StripANSI(view)
	for _, want := range []string{"> Quit", "Exit Spark.", "Default profile: ollama"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected snapshot to contain %q, got %q", want, plain)
		}
	}
}
