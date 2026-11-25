package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/mmo-con/headquarters/internal/domain"
)

type column struct {
	title string
	width int
	// right aligns a count column; hidden columns take no space.
	right, hidden bool
}

// colSpec bounds a column: min is the hard minimum, pref the width reserved
// even for shorter content (so transient states do not shift the table), and
// cap the width beyond which content is only shown if space is left over.
// A count column has a short title, its symbol, for narrow tables.
type colSpec struct {
	title          string
	min, pref, cap int
	short          func(icons) string
}

// Column indexes of the repository table.
const (
	colMark = iota
	colHealth
	colName
	colFamily
	colBranch
	colState
	colStaged
	colModified
	colUntracked
	colStashed
	colAhead
	colBehind
	colCommit
)

var colSpecs = []colSpec{
	colMark: {title: "", min: 3, pref: 3, cap: 3}, colHealth: {title: "", min: 1, pref: 1, cap: 6},
	colName:   {title: "Repository", min: 4, pref: 8, cap: 38},
	colFamily: {title: "Family", min: 4, pref: 6, cap: 20}, colBranch: {title: "Branch", min: 6, pref: 10, cap: 40},
	colState:     {title: "State", min: 6, pref: 16, cap: 40},
	colStaged:    {title: "Staged", min: 1, cap: 9, short: func(ic icons) string { return ic.staged }},
	colModified:  {title: "Modified", min: 1, cap: 9, short: func(ic icons) string { return ic.modified }},
	colUntracked: {title: "Untracked", min: 1, cap: 9, short: func(ic icons) string { return ic.untracked }},
	colStashed:   {title: "Stashed", min: 1, cap: 9, short: func(ic icons) string { return ic.stash }},
	colAhead:     {title: "Ahead", min: 1, cap: 9, short: func(ic icons) string { return ic.ahead }},
	colBehind:    {title: "Behind", min: 1, cap: 9, short: func(ic icons) string { return ic.behind }},
	colCommit:    {title: "Commit", min: 6, pref: 6, cap: 6},
}

// columns sizes the columns of a table of inner width iw to their content.
// natural holds the widest cell of each column. The family column is hidden
// if hideFamily is set: in the tree, whose family rows name it, and with a
// family filter, which gives every row the same family. Count columns use their full
// titles if the other columns stay readable, otherwise their symbols.
func columns(iw int, natural []int, ic icons, hideFamily bool) []column {
	cols, comfortable := layoutColumns(iw, natural, ic, hideFamily, false)
	if comfortable {
		return cols
	}
	cols, _ = layoutColumns(iw, natural, ic, hideFamily, true)
	return cols
}

// layoutColumns sizes the columns with full or short count titles. Space left
// over stays empty at the end of a row; if space is short, columns shrink in
// stages so that the state shrinks last. comfortable reports whether the
// table fits without shrinking name, branch, state, or family far.
func layoutColumns(iw int, natural []int, ic icons, hideFamily, short bool) (cols []column, comfortable bool) {
	cols = make([]column, len(colSpecs))
	want := make([]int, len(colSpecs))
	for i, s := range colSpecs {
		title := s.title
		if short && s.short != nil {
			title = s.short(ic)
		}
		want[i] = max(natural[i], runewidth.StringWidth(title), s.pref)
		cols[i] = column{title: title, width: min(want[i], s.cap), right: s.short != nil}
	}
	if hideFamily {
		cols[colFamily] = column{hidden: true}
	}
	free := iw - tableWidth(cols)
	for _, i := range []int{colBranch, colName, colState, colFamily} {
		if cols[i].hidden {
			continue
		}
		add := max(min(free, want[i]-cols[i].width), 0)
		cols[i].width += add
		free -= add
	}
	for _, s := range []struct{ col, floor int }{
		{colBranch, 20}, {colName, 18}, {colFamily, 10}, {colBranch, 14}, {colName, 12}, {colState, 14},
		{colFamily, 6}, {colBranch, 8}, {colName, 8}, {colFamily, 4}, {colBranch, 6},
		{colName, 4}, {colState, 8}, {colState, 6},
	} {
		for free < 0 && !cols[s.col].hidden && cols[s.col].width > max(s.floor, colSpecs[s.col].min) {
			cols[s.col].width--
			free++
		}
	}
	comfortable = free >= 0
	for _, s := range []struct{ col, floor int }{{colName, 12}, {colBranch, 14}, {colState, 14}, {colFamily, 8}} {
		if !cols[s.col].hidden && cols[s.col].width < min(want[s.col], s.floor) {
			comfortable = false
		}
	}
	return cols, comfortable
}

// rowCells returns the cells of r without the cursor and mark column. The
// health symbol has its own column at the left edge, so the most important
// state of every row is in one place and all names start in the same column.
// Only the names of repositories with errors or critical states are
// emphasized, so they stand out among rows that merely need attention.
func (m *Model) rowCells(r *domain.Repository, now time.Time) [][]seg {
	age := seg{"", m.theme.dim}
	if r.LastCommit.Valid && r.LastCommit.Value != nil {
		age.text = commitAge(now, r.LastCommit.Value.Time)
	}
	health, _ := m.health(r)
	name := m.theme.plain
	if l := m.healthOf(r); l == healthError || l == healthCritical {
		name = m.theme.bold
	}
	return append([][]seg{
		{health},
		{{r.Name, name}},
		{{string(r.Family), m.theme.dim}},
		m.branchSegs(r, false),
		m.stateSegs(r),
	}, append(m.countSegs(r), []seg{age})...)
}

// repoTable holds the cells of all rows of the repository table. All rows
// are measured, not only the visible ones, so the widths do not change on
// scrolling.
type repoTable struct {
	now time.Time
	// cells holds the cells of a repository row from the health column on;
	// family holds the toggle, name, and count of a family row of the tree
	// without the cursor, and summary its health summary.
	cells   [][][]seg
	family  [][]seg
	summary [][]seg
	natural []int
	// familyWidth is the width of the widest family row with its cursor.
	familyWidth int
}

func (m *Model) measureTable() repoTable {
	t := repoTable{now: m.now(), cells: make([][][]seg, len(m.rows)), family: make([][]seg, len(m.rows)),
		summary: make([][]seg, len(m.rows)), natural: make([]int, len(colSpecs))}
	var health map[domain.FamilyID][]healthLevel
	if m.tree {
		health = m.familyHealth()
	}
	for i, row := range m.rows {
		if row.count > 0 {
			// The toggle takes the mark column, so family names start left of
			// the tree guides and stand out from repository names. Expanded
			// families show the health in their rows instead of a summary.
			toggle := m.icons.collapsed
			expanded := m.isExpanded(row.family)
			if expanded {
				toggle = m.icons.expanded
			}
			t.family[i] = []seg{
				{" " + toggle + " ", m.theme.dim}, {string(row.family), m.theme.title},
				{fmt.Sprintf(" (%d)", row.count), m.theme.dim},
			}
			if !expanded {
				t.summary[i] = m.familySummary(health[row.family])
			}
			w := 1 + runewidth.StringWidth(domain.Printable(plain(t.family[i])))
			if len(t.summary[i]) > 0 {
				w += familySummaryGap + runewidth.StringWidth(domain.Printable(plain(t.summary[i])))
			}
			t.familyWidth = max(t.familyWidth, w)
			continue
		}
		t.cells[i] = m.rowCells(m.repo(row.id), t.now)
		if m.tree {
			guide := m.icons.treeItem
			if i+1 == len(m.rows) || m.rows[i+1].count > 0 {
				guide = m.icons.treeLast
			}
			t.cells[i][0] = append([]seg{{guide, m.theme.dim}}, t.cells[i][0]...)
		}
		for j, c := range t.cells[i] {
			t.natural[j+colHealth] = max(t.natural[j+colHealth], runewidth.StringWidth(plain(c)))
		}
	}
	return t
}

// tableNeed is the width of a repository panel that shows the table without
// shrinking or space left over.
func (m *Model) tableNeed(t repoTable) int {
	cols := columns(math.MaxInt/2, t.natural, m.icons, m.hideFamily())
	return max(tableWidth(cols), t.familyWidth) + 2
}

// reposBox renders the repository table in a panel of size w×h. The
// selection is highlighted to the right edge of the table, not of the panel.
func (m *Model) reposBox(t repoTable, w, h int, active bool) []string {
	iw := w - 2
	cells := t.cells
	cols := columns(iw, t.natural, m.icons, m.hideFamily())
	tw := min(tableWidth(cols), iw)
	var lines []string
	rows := h - 3
	var header strings.Builder
	for j, c := range cols {
		if c.hidden {
			continue
		}
		header.WriteString(strings.Repeat(" ", colGap(j)) + align(c.title, c.width, c.right))
	}
	lines = append(lines, m.theme.tableHeader.Render(header.String()))
	if len(m.rows) == 0 {
		lines = append(lines, m.theme.dim.Render(domain.Printable(m.emptyText())))
	}
	for i := m.repoTop; i < len(m.rows) && i < m.repoTop+rows; i++ {
		entry := m.rows[i]
		base := m.theme.plain
		cursor := " "
		if i == m.cursor {
			base = m.theme.selected
			cursor = m.icons.cursor
		}
		if entry.count > 0 {
			ss := append([]seg{{cursor, m.theme.accent}}, t.family[i]...)
			if summary := t.summary[i]; len(summary) > 0 {
				// The summary starts in the State column, like the state of
				// the repositories, unless the family name reaches into it.
				gap := max(stateStart(cols)-runewidth.StringWidth(domain.Printable(plain(ss))), familySummaryGap)
				ss = append(append(ss, seg{strings.Repeat(" ", gap), m.theme.plain}), summary...)
			}
			if i == m.cursor {
				for k := range ss {
					ss[k].style = m.theme.onSelected(ss[k].style)
				}
			}
			w := min(max(tw, runewidth.StringWidth(domain.Printable(plain(ss)))), iw)
			lines = append(lines, renderCell(ss, w, base))
			continue
		}
		mark := seg{" ", m.theme.plain}
		if m.marks[entry.id] {
			mark = seg{m.icons.mark, m.theme.key}
		}
		row := append([][]seg{{{cursor, m.theme.accent}, {" ", m.theme.plain}, mark}}, cells[i]...)
		if i == m.cursor {
			for _, cell := range row {
				for k := range cell {
					cell[k].style = m.theme.onSelected(cell[k].style)
				}
			}
		}
		var b strings.Builder
		for j, c := range cols {
			if c.hidden {
				continue
			}
			if gap := colGap(j); gap > 0 {
				b.WriteString(base.Render(strings.Repeat(" ", gap)))
			}
			w := c.width
			if c.right {
				if pad := w - runewidth.StringWidth(domain.Printable(plain(row[j]))); pad > 0 {
					b.WriteString(base.Render(strings.Repeat(" ", pad)))
					w -= pad
				}
			}
			b.WriteString(renderCell(row[j], w, base))
		}
		lines = append(lines, b.String())
	}

	title := "Repositories"
	if m.tree {
		title += " (tree)"
	}
	if m.filter.Family != "" {
		title += " ─ " + string(m.filter.Family)
	}
	if m.filter.Text != "" {
		title += " ─ /" + m.filter.Text
	}
	footer := ""
	if m.tree {
		footer = fmt.Sprintf("%d repos shown", len(m.visible))
	} else if len(m.visible) > 0 {
		footer = fmt.Sprintf("%d of %d", m.cursor+1, len(m.visible))
	}
	if len(m.visible) != len(m.repos) {
		footer += fmt.Sprintf(" (%d total)", len(m.repos))
	}
	if n := len(m.marks); n > 0 {
		marks := fmt.Sprintf("%d marked · A: clear marks", n)
		if footer != "" {
			marks += " │ " + footer
		}
		footer = marks
	}
	return m.box(title, strings.TrimSpace(footer), lines, w, h, active)
}

// colGap is the space before column j. Columns are separated by three
// spaces, more than the two between the elements of a cell; the health
// symbol and the name are closer to the cursor and mark, which belong to
// them, and the narrow count columns form a block with two spaces between
// them.
func colGap(j int) int {
	switch {
	case j == colMark:
		return 0
	case j == colHealth, j == colName:
		return 1
	case colSpecs[j].short != nil:
		return 2
	}
	return 3
}

// hideFamily reports whether the family column is hidden: in the tree and
// with a family filter.
func (m *Model) hideFamily() bool { return m.tree || m.filter.Family != "" }

// tableWidth is the width of a table row with its column gaps.
func tableWidth(cols []column) int {
	w := 0
	for j, c := range cols {
		if !c.hidden {
			w += colGap(j) + c.width
		}
	}
	return w
}

// familySummaryGap is the minimum space between a family name and its health
// summary.
const familySummaryGap = 3

// stateStart is the column at which the State column starts in a row.
func stateStart(cols []column) int {
	return tableWidth(cols[:colState]) + colGap(colState)
}

// align truncates or pads s to w cells, on the left if right is set.
func align(s string, w int, right bool) string {
	s = fitANSI(s, w)
	if right {
		s = strings.TrimRight(s, " ")
		s = strings.Repeat(" ", max(w-ansi.StringWidth(s), 0)) + s
	}
	return s
}

func (m *Model) emptyText() string {
	switch {
	case m.loadingInventory:
		return " " + m.spinnerFrame() + " loading inventory…"
	case len(m.repos) == 0 && m.app.ScanRoot != "":
		return fmt.Sprintf(" no repositories found below %s", m.app.ScanRoot)
	case len(m.repos) == 0 && m.app.Config.File == "":
		return fmt.Sprintf(" no configuration found at %s; see examples/config.toml", m.app.ConfigPath)
	case len(m.repos) == 0:
		return " the configuration contains no repositories"
	}
	return " no repository matches the current filter (Esc clears text, t chooses family)"
}

func line(ss ...seg) string {
	var b strings.Builder
	for _, s := range ss {
		b.WriteString(s.style.Render(domain.Printable(s.text)))
	}
	return b.String()
}
