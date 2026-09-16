package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Design Tokens - Modernized Tokyo Night / Midnight Slate Palette
var (
	// Brand & Focus Accents
	colorFocus        = lipgloss.Color("#8b5cf6") // Vibrant Violet Focus
	colorAccent       = lipgloss.Color("#38bdf8") // Sky Cyan Accent
	colorBrand        = lipgloss.Color("#a78bfa") // Light Violet

	// Neutral Text Hierarchy
	colorText         = lipgloss.Color("#f8fafc") // Crisp Primary Text
	colorTextSoft     = lipgloss.Color("#cbd5e1") // Secondary Readable Text
	colorLabel        = lipgloss.Color("#94a3b8") // Dimmer Label Text
	colorMuted        = lipgloss.Color("#64748b") // Helper & Hint Muted Text
	colorDim          = colorMuted

	// Semantic Status Indicators
	colorSuccess      = lipgloss.Color("#34d399") // Emerald Mint Green
	colorError        = lipgloss.Color("#f87171") // Coral Rose Red
	colorWarning      = lipgloss.Color("#fbbf24") // Warm Sun Amber

	// Dark Surface & Inset Layers
	colorBg           = lipgloss.Color("#0f172a") // Deep Midnight Dark
	colorPanelBg      = lipgloss.Color("#1e293b") // Elevated Slate Panel
	colorBorder       = lipgloss.Color("#334155") // Subtle Border
	colorBorderFocus  = lipgloss.Color("#8b5cf6") // Active Glow Border
	colorFieldBg      = lipgloss.Color("#0f172a") // Inset Input Box
	colorFieldBgFocus = lipgloss.Color("#1e293b") // Active Field Inset
)

// Shared Component Styles
var (
	// App & Layout Containers
	appContainerStyle = lipgloss.NewStyle().Margin(0, 1)

	headerBannerStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true).
				Padding(0, 1)

	panelContainerStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorder).
				Padding(0, 1)

	panelFocusedContainerStyle = panelContainerStyle.Copy().
					BorderForeground(colorBorderFocus)

	// Typography & Labels
	sectionHeaderStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	accentTitleStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	dimSubtextStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	mutedHelpStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	// List Items & Selections
	menuItemStyle = lipgloss.NewStyle().
			PaddingLeft(1).
			Foreground(colorTextSoft)

	menuSelectedStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(colorText).
				Bold(true)

	menuFocusedSelectedStyle = lipgloss.NewStyle().
					PaddingLeft(1).
					Foreground(colorFocus).
					Bold(true)

	// Badges & Status Indicators
	badgeSuccessStyle = lipgloss.NewStyle().
				Foreground(colorSuccess).
				Bold(true)

	badgeErrorStyle = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	badgeWarningStyle = lipgloss.NewStyle().
				Foreground(colorWarning).
				Bold(true)

	// Modals & Overlays
	dialogOverlayStyle = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(colorFocus).
				Padding(1, 2).
				Align(lipgloss.Center)
)

func errorStatus(s string) string {
	return "Error: " + s
}

func successStatus(s string) string {
	return s
}

func infoStatus(s string) string {
	return s
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
