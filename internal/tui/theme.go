package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/mmo-con/headquarters/internal/config"
)

// theme holds the styles derived from the configured colors.
type theme struct {
	activeBorder   lipgloss.Style
	inactiveBorder lipgloss.Style
	title          lipgloss.Style
	panelTitle     lipgloss.Style
	selected       lipgloss.Style
	accent         lipgloss.Style
	header         lipgloss.Style
	tableHeader    lipgloss.Style
	key            lipgloss.Style
	branch         lipgloss.Style
	clean          lipgloss.Style
	dirty          lipgloss.Style
	ahead          lipgloss.Style
	behind         lipgloss.Style
	err            lipgloss.Style
	warn           lipgloss.Style
	success        lipgloss.Style
	dim            lipgloss.Style
	bold           lipgloss.Style
	plain          lipgloss.Style
	backdrop       lipgloss.Style

	dimColor, selectedFg lipgloss.Color
}

func newTheme(t config.Theme) theme {
	c := func(v string) lipgloss.Color { return lipgloss.Color(config.ResolveColor(v)) }
	fg := func(v string) lipgloss.Style { return lipgloss.NewStyle().Foreground(c(v)) }
	return theme{
		activeBorder:   fg(t.ActiveBorder),
		inactiveBorder: fg(t.InactiveBorder),
		title:          fg(t.Title).Bold(true),
		panelTitle:     fg(t.ActiveBorder).Bold(true),
		selected:       lipgloss.NewStyle().Background(c(t.SelectedBackground)).Foreground(c(t.SelectedForeground)).Bold(true),
		accent:         fg(t.SelectedAccent).Bold(true),
		header:         fg(t.Dim).Bold(true),
		tableHeader:    lipgloss.NewStyle().Background(c(t.HeaderBackground)).Foreground(c(t.HeaderForeground)).Bold(true),
		key:            fg(t.Key).Bold(true),
		branch:         fg(t.Branch),
		clean:          fg(t.Clean),
		dirty:          fg(t.Dirty),
		ahead:          fg(t.Ahead),
		behind:         fg(t.Behind),
		err:            fg(t.Error),
		warn:           fg(t.Warning),
		success:        fg(t.Success),
		dim:            fg(t.Dim),
		bold:           lipgloss.NewStyle().Bold(true),
		plain:          lipgloss.NewStyle(),
		backdrop:       fg(t.Dim).Faint(true),
		dimColor:       c(t.Dim),
		selectedFg:     c(t.SelectedForeground),
	}
}

// onSelected adapts a segment style to the selected row. Secondary and
// unstyled text, and text in the terminal's default foreground, takes the
// selected foreground, so it stays readable on any selected background;
// other semantic colors are kept.
func (t theme) onSelected(s lipgloss.Style) lipgloss.Style {
	if fg := s.GetForeground(); fg == t.dimColor || fg == (lipgloss.NoColor{}) || fg == lipgloss.Color("") {
		s = s.Foreground(t.selectedFg)
	}
	return s
}
