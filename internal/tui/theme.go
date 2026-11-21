package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/mmo-con/git-dashboard/internal/config"
)

// theme holds the styles derived from the configured colors.
type theme struct {
	activeBorder   lipgloss.Style
	inactiveBorder lipgloss.Style
	title          lipgloss.Style
	panelTitle     lipgloss.Style
	selected       lipgloss.Style
	header         lipgloss.Style
	key            lipgloss.Style
	branch         lipgloss.Style
	clean          lipgloss.Style
	dirty          lipgloss.Style
	ahead          lipgloss.Style
	behind         lipgloss.Style
	err            lipgloss.Style
	warn           lipgloss.Style
	ok             lipgloss.Style
	dim            lipgloss.Style
	bold           lipgloss.Style
	plain          lipgloss.Style
	backdrop       lipgloss.Style
}

func newTheme(t config.Theme) theme {
	c := func(v string) lipgloss.Color { return lipgloss.Color(config.ResolveColor(v)) }
	fg := func(v string) lipgloss.Style { return lipgloss.NewStyle().Foreground(c(v)) }
	return theme{
		activeBorder:   fg(t.ActiveBorder),
		inactiveBorder: fg(t.InactiveBorder),
		title:          fg(t.Title).Bold(true),
		panelTitle:     fg(t.ActiveBorder).Bold(true),
		selected:       lipgloss.NewStyle().Background(c(t.SelectedBackground)).Bold(true),
		header:         fg(t.Dim).Bold(true),
		key:            fg(t.Key).Bold(true),
		branch:         fg(t.Branch),
		clean:          fg(t.Clean),
		dirty:          fg(t.Dirty),
		ahead:          fg(t.Ahead),
		behind:         fg(t.Behind),
		err:            fg(t.Error),
		warn:           fg(t.Warning),
		ok:             fg(t.Ahead),
		dim:            fg(t.Dim),
		bold:           lipgloss.NewStyle().Bold(true),
		plain:          lipgloss.NewStyle(),
		backdrop:       fg(t.Dim).Faint(true),
	}
}
