package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/mmo-con/headquarters/internal/domain"
)

// seg is a styled piece of text within a cell.
type seg struct {
	text  string
	style lipgloss.Style
}

func plain(ss []seg) string {
	var b strings.Builder
	for _, s := range ss {
		b.WriteString(s.text)
	}
	return b.String()
}

// renderCell truncates the segments to width w, pads them, and renders them
// on top of base, so a selected row keeps its background across segments.
func renderCell(ss []seg, w int, base lipgloss.Style) string {
	var b strings.Builder
	left := w
	for _, s := range ss {
		if left <= 0 {
			break
		}
		text := domain.Printable(s.text)
		if runewidth.StringWidth(text) > left {
			text = runewidth.Truncate(text, left, "…")
		}
		left -= runewidth.StringWidth(text)
		b.WriteString(s.style.Inherit(base).Render(text))
	}
	if left > 0 {
		b.WriteString(base.Render(strings.Repeat(" ", left)))
	}
	return b.String()
}

// fitANSI truncates or pads a rendered string to exactly w cells.
func fitANSI(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// box draws a rounded frame of size w×h with a title embedded in the top
// border and optional info in the bottom border.
func (m *Model) box(title, footer string, lines []string, w, h int, active bool) []string {
	if w < 4 || h < 2 {
		return nil
	}
	border := m.theme.inactiveBorder
	titleStyle := m.theme.header
	if active {
		border = m.theme.activeBorder
		titleStyle = m.theme.panelTitle
	}
	inner := w - 2
	out := make([]string, 0, h)

	top := ""
	if title != "" {
		t := ansi.Truncate(" "+domain.Printable(title)+" ", inner-2, "…")
		top = border.Render("╭─") + titleStyle.Render(ansi.Strip(t))
		top += border.Render(strings.Repeat("─", max(inner-1-ansi.StringWidth(t), 0)) + "╮")
	} else {
		top = border.Render("╭" + strings.Repeat("─", inner) + "╮")
	}
	out = append(out, top)

	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out = append(out, border.Render("│")+fitANSI(line, inner)+border.Render("│"))
	}

	bottom := ""
	if footer != "" {
		f := ansi.Truncate(" "+domain.Printable(footer)+" ", inner-2, "…")
		bottom = border.Render("╰"+strings.Repeat("─", max(inner-1-ansi.StringWidth(f), 0))) +
			m.theme.dim.Render(ansi.Strip(f)) + border.Render("─╯")
	} else {
		bottom = border.Render("╰" + strings.Repeat("─", inner) + "╯")
	}
	return append(out, bottom)
}

// joinColumns places two blocks of lines side by side.
func joinColumns(a, b []string) []string {
	n := max(len(a), len(b))
	out := make([]string, n)
	for i := 0; i < n; i++ {
		if i < len(a) {
			out[i] = a[i]
		}
		if i < len(b) {
			out[i] += b[i]
		}
	}
	return out
}

// overlay draws the modal centered over a dimmed copy of the background.
func (m *Model) overlay(background, modal []string) []string {
	if len(modal) == 0 {
		return background
	}
	mw := ansi.StringWidth(modal[0])
	x := max((m.width-mw)/2, 0)
	y := max((len(background)-len(modal))/2, 0)
	out := make([]string, len(background))
	for i, line := range background {
		bg := ansi.Strip(line)
		j := i - y
		if j < 0 || j >= len(modal) {
			out[i] = m.theme.backdrop.Render(bg)
			continue
		}
		left := runewidth.FillRight(runewidth.Truncate(bg, x, ""), x)
		right := skipWidth(bg, x+mw)
		out[i] = m.theme.backdrop.Render(left) + modal[j] + m.theme.backdrop.Render(right)
	}
	return out
}

// skipWidth returns s without its first n display cells.
func skipWidth(s string, n int) string {
	w := 0
	for i, r := range s {
		if w >= n {
			return s[i:]
		}
		w += runewidth.RuneWidth(r)
	}
	return ""
}

// progressBar renders done/total as a bar of width w.
func (m *Model) progressBar(done, total, w int) string {
	if total <= 0 || w <= 0 {
		return ""
	}
	full := done * w / total
	return m.theme.success.Render(strings.Repeat(m.icons.barFull, full)) +
		m.theme.dim.Render(strings.Repeat(m.icons.barEmpty, w-full))
}
