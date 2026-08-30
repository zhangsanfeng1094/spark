package tui

import tea "github.com/charmbracelet/bubbletea"

// ScreenBackKeys are keys that leave a manager root screen and return to the
// interactive dashboard loop (or exit a standalone picker).
//
// Nested modes (editor/transfer/confirm) should cancel themselves on esc
// before these keys are considered. ctrl+c always forces quit at the Update
// layer when managers choose to handle it that way.
const screenBackHelp = "Esc/Q Back"

// quitOnScreenBack returns tea.Quit when the key is a manager-root back key.
func quitOnScreenBack(key string) (tea.Cmd, bool) {
	switch key {
	case "esc", "q", "ctrl+c":
		return tea.Quit, true
	default:
		return nil, false
	}
}
