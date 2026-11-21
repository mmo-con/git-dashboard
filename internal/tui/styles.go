package tui

import "github.com/charmbracelet/lipgloss"

var (
	styleTitle    = lipgloss.NewStyle().Bold(true).Reverse(true)
	styleHeader   = lipgloss.NewStyle().Bold(true).Underline(true)
	styleSelected = lipgloss.NewStyle().Reverse(true)
	styleDim      = lipgloss.NewStyle().Faint(true)
	styleError    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleWarn     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleOK       = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleKey      = lipgloss.NewStyle().Bold(true)
	styleBox      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)
