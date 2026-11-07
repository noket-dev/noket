// Package ui holds the terminal styles shared by the CLI and the TUI.
package ui

import "github.com/charmbracelet/lipgloss"

var (
	accent = lipgloss.AdaptiveColor{Light: "30", Dark: "14"}

	Bold   = lipgloss.NewStyle().Bold(true)
	OK     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "10"})
	Warn   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "11"})
	Err    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "9"})
	Faint  = lipgloss.NewStyle().Faint(true)
	Accent = lipgloss.NewStyle().Foreground(accent)

	// Popup frames a modal panel in the TUI. Width, when set, covers the
	// content and the padding but not the border; the one-line vertical
	// padding keeps the form off the frame on both sides.
	Popup = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, 2)
)
