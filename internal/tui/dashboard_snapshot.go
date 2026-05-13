package tui

import (
	"fmt"
	"regexp"
	"strings"

	"spark/internal/config"
	"spark/internal/skills"
)

var ansiControlPattern = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]|\x1b\\][^\a]*(\a|\x1b\\\\)")

func RenderDashboardSnapshot(title string, actions []DashboardAction, summary DashboardSummary, width, height, cursor int) (string, error) {
	if len(actions) == 0 {
		return "", fmt.Errorf("no actions")
	}
	width, height = normalizeSnapshotSize(width, height)
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(actions) {
		cursor = len(actions) - 1
	}

	m := &dashboardModel{
		title:   title,
		actions: actions,
		summary: summary,
		cursor:  cursor,
		width:   width,
		height:  height,
	}
	return m.View(), nil
}

func RenderProfileManagerSnapshot(cfg *config.RootConfig, width, height int) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config is required")
	}
	width, height = normalizeSnapshotSize(width, height)
	m := newPMModel(cfg)
	m.width = width
	m.height = height
	return m.View(), nil
}

func RenderMCPManagerSnapshot(cfg *config.RootConfig, width, height int, state string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config is required")
	}
	width, height = normalizeSnapshotSize(width, height)
	m := newMCPManagerModel(cfg)
	m.width = width
	m.height = height

	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", "overview":
	case "add", "add-stdio":
		m.startAddEditor("stdio")
	case "add-http":
		m.startAddEditor("http")
	case "add-sse":
		m.startAddEditor("sse")
	case "edit", "edit-current":
		m.startEditCurrent()
	case "transfer":
		m.openTransferMenu()
	default:
		return "", fmt.Errorf("unknown mcp snapshot state: %s", state)
	}
	return m.View(), nil
}

func RenderSkillManagerSnapshot(registry *skills.Registry, width, height int, state string) (string, error) {
	width, height = normalizeSnapshotSize(width, height)
	m := newSkillManagerModel(registry)
	m.width = width
	m.height = height

	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", "overview":
	case "install":
		m.openInstallModal()
	case "catalog":
		m.openCatalogModal()
	case "transfer":
		m.openTransferMenu()
	default:
		return "", fmt.Errorf("unknown skill snapshot state: %s", state)
	}
	return m.View(), nil
}

func StripANSI(s string) string {
	return ansiControlPattern.ReplaceAllString(s, "")
}

func normalizeSnapshotSize(width, height int) (int, int) {
	if width < 60 {
		width = 60
	}
	if height < 1 {
		height = 1
	}
	return width, height
}
