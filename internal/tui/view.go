package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/mmo-con/git-dashboard/internal/app"
	"github.com/mmo-con/git-dashboard/internal/domain"
)

// wideLayout is the minimum width for table and details side by side.
const wideLayout = 130

func (m *Model) View() string {
	main := m.mainArea(max(m.height-2, 4))
	if modal := m.modal(len(main)); modal != nil {
		main = m.overlay(main, modal)
	}
	lines := append([]string{m.headerLine()}, main...)
	lines = append(lines, m.footerLine())
	return strings.Join(lines, "\n")
}

// mainArea returns the repository and detail panels for h lines.
func (m *Model) mainArea(h int) []string {
	active := m.mode == modeNormal || m.mode == modeFilter
	if m.width >= wideLayout {
		rw := m.width * 60 / 100
		return joinColumns(m.reposBox(rw, h, active), m.detailsBox(m.width-rw, h))
	}
	dh := stackedDetailsHeight(h)
	return append(m.reposBox(m.width, h-dh, active), m.detailsBox(m.width, dh)...)
}

func stackedDetailsHeight(h int) int {
	if h < 20 {
		return max(h/3, 4)
	}
	return 9
}

// tableRows is the number of repository rows that fit into the table.
func (m *Model) tableRows() int {
	h := max(m.height-2, 4)
	if m.width < wideLayout {
		h -= stackedDetailsHeight(h)
	}
	return max(h-3, 1)
}

func (m *Model) pageSize() int { return max(m.tableRows()-1, 1) }

type column struct {
	title string
	width int
}

// colSpec bounds a column: min is the hard minimum, pref the width reserved
// even for shorter content (so transient states do not shift the table), and
// cap the width beyond which content is only shown if space is left over.
type colSpec struct {
	title          string
	min, pref, cap int
}

// Column indexes of the repository table.
const (
	colMark = iota
	colName
	colFamily
	colBranch
	colStatus
	colUpstream
	colCommit
)

var colSpecs = []colSpec{
	colMark: {"", 3, 3, 3}, colName: {"Repository", 6, 10, 40},
	colFamily: {"Family", 4, 6, 20}, colBranch: {"Branch", 6, 10, 40}, colStatus: {"Status", 6, 9, 30},
	colUpstream: {"Upstream", 6, 16, 30}, colCommit: {"Commit", 3, 6, 6},
}

// columns sizes the columns of a table of inner width iw to their content.
// natural holds the widest cell of each column. Space left over stays empty
// at the end of a row; if space is short, columns shrink in stages so that
// status and upstream shrink last.
func columns(iw int, natural []int) []column {
	cols := make([]column, len(colSpecs))
	want := make([]int, len(colSpecs))
	free := iw - tableWidth(make([]column, len(colSpecs)))
	for i, s := range colSpecs {
		want[i] = max(natural[i], runewidth.StringWidth(s.title), s.pref)
		cols[i] = column{s.title, min(want[i], s.cap)}
		free -= cols[i].width
	}
	for _, i := range []int{colBranch, colName, colStatus, colUpstream, colFamily} {
		add := max(min(free, want[i]-cols[i].width), 0)
		cols[i].width += add
		free -= add
	}
	for _, s := range []struct{ col, floor int }{
		{colBranch, 20}, {colName, 20}, {colFamily, 10}, {colBranch, 14}, {colName, 14}, {colUpstream, 14},
		{colStatus, 12}, {colFamily, 6}, {colBranch, 8}, {colName, 10}, {colCommit, 3}, {colFamily, 4}, {colBranch, 6},
		{colName, 6}, {colUpstream, 8}, {colStatus, 8}, {colUpstream, 6}, {colStatus, 6},
	} {
		for free < 0 && cols[s.col].width > max(s.floor, colSpecs[s.col].min) {
			cols[s.col].width--
			free++
		}
	}
	return cols
}

// rowCells returns the cells of r without the cursor and mark column. The
// name starts with the health symbol, so the symbol is always next to the
// name and the most important state of every row is at its left edge. The
// names of healthy repositories are not emphasized, so rows that need
// attention stand out.
func (m *Model) rowCells(r *domain.Repository, now time.Time) [][]seg {
	age := seg{"", m.theme.dim}
	if r.LastCommit.Valid && r.LastCommit.Value != nil {
		age.text = shortAgo(now, r.LastCommit.Value.Time)
	}
	health, healthy := m.health(r)
	if health.text == "" {
		health.text = " "
	}
	name := m.theme.bold
	if healthy {
		name = m.theme.plain
	}
	return [][]seg{
		{health, {" ", m.theme.plain}, {r.Name, name}},
		{{string(r.Family), m.theme.dim}},
		m.branchSegs(r),
		m.statusSegs(r),
		append(m.upstreamSegs(r), m.branchesMarker(r)...),
		{age},
	}
}

func (m *Model) reposBox(w, h int, active bool) []string {
	iw := w - 2
	now := m.now()
	// All visible rows are measured, so the widths do not change on scrolling.
	cells := make([][][]seg, len(m.rows))
	natural := make([]int, len(colSpecs))
	for i, row := range m.rows {
		if row.count > 0 {
			continue
		}
		cells[i] = m.rowCells(m.repo(row.id), now)
		if m.tree {
			guide := m.icons.treeItem
			if i+1 == len(m.rows) || m.rows[i+1].count > 0 {
				guide = m.icons.treeLast
			}
			cells[i][0] = append([]seg{{guide, m.theme.dim}}, cells[i][0]...)
		}
		for j, c := range cells[i] {
			natural[j+colName] = max(natural[j+colName], runewidth.StringWidth(plain(c)))
		}
	}
	cols := columns(iw, natural)
	var health map[domain.FamilyID][]healthLevel
	if m.tree {
		health = m.familyHealth()
	}
	var lines []string
	var header strings.Builder
	for j, c := range cols {
		header.WriteString(strings.Repeat(" ", colGap(j)) + fitANSI(c.title, c.width))
	}
	lines = append(lines, m.theme.header.Render(header.String()))

	rows := h - 3
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
			toggle := m.icons.collapsed
			if m.expanded[entry.family] {
				toggle = m.icons.expanded
			}
			ss := []seg{
				{cursor, m.theme.accent}, {"   " + toggle + " ", m.theme.dim}, {string(entry.family), m.theme.bold},
				{fmt.Sprintf(" (%d)", entry.count), m.theme.dim},
			}
			if summary := m.familySummary(health[entry.family]); len(summary) > 0 {
				ss = append(append(ss, seg{"   ", m.theme.plain}), summary...)
			}
			if i == m.cursor {
				for k := range ss {
					ss[k].style = m.theme.onSelected(ss[k].style)
				}
			}
			lines = append(lines, renderCell(ss, iw, base))
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
			if gap := colGap(j); gap > 0 {
				b.WriteString(base.Render(strings.Repeat(" ", gap)))
			}
			b.WriteString(renderCell(row[j], c.width, base))
		}
		b.WriteString(base.Render(strings.Repeat(" ", max(iw-tableWidth(cols), 0))))
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
// spaces, more than the two between the elements of a cell; the name is
// closer to the cursor and mark, which belong to it.
func colGap(j int) int {
	switch j {
	case colMark:
		return 0
	case colName:
		return 1
	}
	return 3
}

// tableWidth is the width of a table row with its column gaps.
func tableWidth(cols []column) int {
	w := 0
	for j, c := range cols {
		w += colGap(j) + c.width
	}
	return w
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

func (m *Model) detailsBox(w, h int) []string {
	if row := m.currentRow(); row.count > 0 {
		action := "expand"
		if m.expanded[row.family] {
			action = "collapse"
		}
		lines := []string{fmt.Sprintf(" Matching repositories: %d", row.count)}
		if summary := m.familySummary(m.familyHealth()[row.family]); len(summary) > 0 {
			lines = append(lines, " "+line(summary...))
		}
		return m.box(string(row.family), "", append(lines, "", " Enter: "+action+" family"), w, h, false)
	}
	r := m.current()
	if r == nil {
		return m.box("Details", "", nil, w, h, false)
	}
	lines := m.detailLines(r).fit(h - 2)
	for i := range lines {
		lines[i] = " " + lines[i]
	}
	return m.box(r.Name, sourceLabel(r), lines, w, h, false)
}

// sourceLabel describes how the repository entered the inventory.
func sourceLabel(r *domain.Repository) string {
	if r.Source != domain.InventorySourceDiscovered {
		return "configured"
	}
	if r.DiscoveryRoot == "" {
		return "discovered"
	}
	return "discovered in " + shortenHome(r.DiscoveryRoot)
}

// shortenHome replaces the home directory at the start of path with ~.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return path
}

type detailKind int

const (
	detailText detailKind = iota
	detailBlank
	detailSection
	// detailBranch is a line of the other branches list, which is shortened
	// before other content is dropped.
	detailBranch
)

// details collects the lines of the detail panel, grouped into sections.
type details struct {
	lines []string
	kinds []detailKind
	// more renders the line that replaces n hidden other branches.
	more func(n int) string
}

func (d *details) add(l string, k detailKind) {
	d.lines = append(d.lines, l)
	d.kinds = append(d.kinds, k)
}

func (d *details) text(ss ...seg) { d.add(line(ss...), detailText) }

// section starts a titled section, separated from the previous one by a
// blank line.
func (d *details) section(title string, style lipgloss.Style) {
	if len(d.lines) > 0 {
		d.add("", detailBlank)
	}
	d.add(style.Render(strings.ToUpper(title)), detailSection)
}

// fit returns at most n lines if possible. At most maxBranchLines other
// branches are listed. If the panel is too small, blank lines are dropped
// first, then the other branches list is shortened, then the section titles
// are dropped.
func (d *details) fit(n int) []string {
	total := 0
	for _, k := range d.kinds {
		if k == detailBranch {
			total++
		}
	}
	render := func(branches int, drop ...detailKind) []string {
		var out []string
		seen := 0
		for i, l := range d.lines {
			k := d.kinds[i]
			if k == detailBranch {
				seen++
				if seen <= branches {
					out = append(out, l)
				}
				if seen == total && total > branches {
					out = append(out, d.more(total-branches))
				}
				continue
			}
			if !slices.Contains(drop, k) {
				out = append(out, l)
			}
		}
		return out
	}
	shown := min(total, maxBranchLines)
	if out := render(shown); len(out) <= n {
		return out
	}
	for _, drop := range [][]detailKind{{detailBlank}, {detailBlank, detailSection}} {
		for b := shown; b >= 0; b-- {
			if out := render(b, drop...); len(out) <= n {
				return out
			}
		}
	}
	return render(0, detailBlank, detailSection)
}

// detailLabelWidth aligns the labels of the detail panel.
const detailLabelWidth = 10

func (m *Model) label(s string) seg {
	return seg{runewidth.FillRight(s, detailLabelWidth), m.theme.dim}
}

// detailLines builds the detail panel. The current branch, the worktree, and
// errors come first, so they stay visible when the panel is cut off at the
// bottom.
func (m *Model) detailLines(r *domain.Repository) *details {
	t := m.theme
	now := m.now()
	d := &details{more: func(n int) string { return line(seg{fmt.Sprintf("  +%d more", n), t.dim}) }}
	d.text(seg{r.Path, t.dim})
	d.text(m.label("family"), seg{string(r.Family), t.key})
	if r.Workspace != "" && r.Workspace != r.Path {
		d.text(m.label("workspace"), seg{r.Workspace, t.dim})
	}

	inspected := r.IsAvailable() && loaded(r)
	if inspected {
		d.section("branch", t.header)
		b := append([]seg{}, m.branchSegs(r)...)
		if r.Upstream.Valid && r.Upstream.Value.Configured {
			b = append(b, seg{" → " + r.Upstream.Value.Ref, t.dim})
		}
		b = append(b, seg{"  ", t.plain})
		d.text(append(b, m.upstreamSegs(r)...)...)

		if r.Worktree.Valid {
			d.section("worktree", t.header)
			if r.Operation.Valid && r.Operation.Value != domain.NoOperation {
				d.text(seg{r.Operation.Value.String() + " in progress", t.err.Bold(true)})
			}
			d.text(m.worktreeSegs(r.Worktree.Value)...)
		}
	}

	if e := m.firstError(r); e != "" {
		d.section("error", t.err.Bold(true))
		d.text(seg{m.icons.failed + " " + firstLine(e), t.err})
		d.text(seg{"press e for details", t.dim})
	}

	if inspected {
		if bs := m.otherBranchLines(r); len(bs) > 0 {
			d.section("other branches", t.header)
			for _, l := range bs {
				d.add(l, detailBranch)
			}
		}

		if r.LastCommit.Valid {
			d.section("last commit", t.header)
			if c := r.LastCommit.Value; c != nil {
				d.text(seg{c.ShortID + "  ", t.warn}, seg{c.Subject, t.plain})
				d.text(seg{c.Author + " · " + ago(now, c.Time), t.dim})
			} else {
				d.text(seg{"no commits yet", t.dim})
			}
			d.text(m.fetchSeg(now, r))
		}

		if r.Remotes.Valid {
			d.section("remotes", t.header)
			nameWidth := 0
			for _, rem := range r.Remotes.Value {
				nameWidth = max(nameWidth, runewidth.StringWidth(domain.Printable(rem.Name)))
			}
			for _, rem := range r.Remotes.Value {
				name := runewidth.FillRight(domain.Printable(rem.Name), nameWidth)
				d.text(seg{name + "  ", t.bold}, seg{domain.RedactURL(rem.URL), t.dim})
			}
			if len(r.Remotes.Value) == 0 {
				d.text(seg{"no remotes", t.dim})
			}
			if r.RemoteMismatch() {
				d.text(seg{m.icons.mismatch + " expected " + remoteName(r.ExpectedRemote) + ": " +
					domain.RedactURL(r.ExpectedRemote.URL), t.warn})
			}
		}

		if siblings := m.worktreeSiblings(r); len(siblings) > 0 {
			d.section("linked worktrees", t.header)
			for _, p := range siblings {
				d.text(seg{p, t.dim})
			}
		}
	}
	return d
}

// worktreeSegs lists the nonzero worktree counts with their symbols.
func (m *Model) worktreeSegs(w domain.WorktreeState) []seg {
	t, ic := m.theme, m.icons
	var out []seg
	for _, c := range []struct {
		icon, label string
		n           int
		style       lipgloss.Style
	}{
		{ic.conflict, "conflicts", w.Conflicts, t.err},
		{ic.staged, "staged", w.Staged, t.ahead},
		{"", "renamed", w.Renamed, t.dim},
		{ic.modified, "modified", w.Modified, t.dirty},
		{ic.deleted, "deleted", w.Deleted, t.dirty},
		{ic.untracked, "untracked", w.Untracked, t.dim},
	} {
		if c.n == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, seg{"   ", t.plain})
		}
		out = append(out, seg{fmt.Sprintf("%s%d %s", c.icon, c.n, c.label), c.style})
	}
	if len(out) == 0 {
		out = append(out, seg{ic.clean + " clean", t.clean})
	}
	if !w.UntrackedChecked {
		out = append(out, seg{"   untracked not checked", t.dim})
	}
	return out
}

func remoteName(e *domain.ExpectedRemote) string {
	if e.Name == "" {
		return "origin"
	}
	return e.Name
}

func (m *Model) worktreeSiblings(r *domain.Repository) []string {
	if r.StoreID == "" {
		return nil
	}
	var out []string
	for i := range m.repos {
		o := &m.repos[i]
		if o.StoreID == r.StoreID && o.ID != r.ID {
			out = append(out, o.Path)
		}
	}
	return out
}

func (m *Model) firstError(r *domain.Repository) string {
	switch {
	case r.EntryError != nil:
		return r.EntryError.Error()
	case r.Inspection.Err != nil:
		return r.Inspection.Err.Error()
	}
	if last := m.lastOp(r.ID); last != nil && last.Error != nil {
		return fmt.Sprintf("%s %s: %v", last.Operation, last.Status, last.Error)
	}
	return ""
}

// quickFilter is a header counter that doubles as a filter shortcut.
type quickFilter struct {
	key, token, label string
	style             lipgloss.Style
}

func (m *Model) quickFilters() []quickFilter {
	t := m.theme
	return []quickFilter{
		{"1", "is:dirty", "dirty", t.dirty},
		{"2", "is:behind", "behind", t.behind},
		{"3", "is:ahead", "ahead", t.ahead},
		{"4", "is:diverged", "diverged", t.err},
		{"5", "is:error", "errors", t.err},
		{"6", "is:branches", "branches", t.warn},
	}
}

func (m *Model) headerLine() string {
	t := m.theme
	left := t.title.Render(" Git Dashboard ")
	scope := app.Filter{Family: m.filter.Family}
	for _, q := range m.quickFilters() {
		n := 0
		f := app.Filter{Family: scope.Family, Text: q.token}
		for i := range m.repos {
			if f.Matches(&m.repos[i], false) {
				n++
			}
		}
		label := fmt.Sprintf("%s %d", q.label, n)
		style := q.style
		if n == 0 {
			style = t.dim
		}
		if strings.TrimSpace(m.filter.Text) == q.token {
			style = style.Reverse(true)
		}
		left += "  " + t.key.Render(q.key) + " " + style.Render(label)
	}

	var right string
	switch {
	case m.loadingInventory:
		right = t.key.Render(m.spinnerFrame()) + " loading inventory"
	case m.batch != nil && !m.batch.done():
		done := 0
		for _, r := range m.batch.rows {
			if r.result.Status.Final() {
				done++
			}
		}
		right = t.key.Render(m.spinnerFrame()) + " " + string(m.batch.op) + " " +
			m.progressBar(done, len(m.batch.rows), 12) + fmt.Sprintf(" %d/%d", done, len(m.batch.rows))
	case m.pendingInspect() > 0:
		right = t.key.Render(m.spinnerFrame()) + fmt.Sprintf(" inspecting %d", m.pendingInspect())
	}
	right += " "
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return fitANSI(left, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) hints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, m.theme.key.Render(pairs[i])+" "+m.theme.dim.Render(pairs[i+1]))
	}
	return strings.Join(parts, m.theme.dim.Render(" │ "))
}

func (m *Model) footerLine() string {
	var left string
	switch m.mode {
	case modeFilter:
		left = m.theme.key.Render(" /") + domain.Printable(m.filter.Text) + m.theme.key.Render("█") + "  " +
			m.hints("Enter", "apply", "Esc", "clear", "Ctrl+U", "erase")
	case modeFamily:
		left = " " + m.hints("↑/↓", "move", "Enter", "choose", "Esc", "cancel", "Ctrl+U", "erase search")
	case modeOperations:
		left = " " + m.hints(m.operationHints()...)
	case modeConfirmFetch:
		left = " " + m.hints("Enter/y", "confirm", "Esc/n", "cancel")
	case modeUpdatePreview:
		left = " " + m.hints("Enter/y", "update", "f", "fetch first", "j/k", "move", "Esc/n", "cancel")
	case modeConfirmQuit:
		left = " " + m.hints("Enter/y", "quit", "Esc/n", "keep running")
	case modeSort, modeLaunchers:
		left = " " + m.hints("j/k", "move", "Enter", "choose", "Esc", "close")
	case modeError:
		left = " " + m.hints("t", "fetch in terminal", "Esc", "close")
	case modeHelp:
		left = " " + m.hints("j/k", "scroll", "any other key", "close")
	default:
		left = " " + m.hints(m.normalHints()...)
	}
	if m.message == "" {
		return fitANSI(left, m.width)
	}
	style, icon := m.theme.success, m.icons.clean
	if m.messageErr {
		style, icon = m.theme.err, m.icons.failed
	}
	msg := " " + icon + " " + m.message + " "
	maxMsg := max(m.width/2, 20)
	if ansi.StringWidth(msg) > maxMsg {
		msg = ansi.Truncate(msg, maxMsg, "…")
	}
	right := style.Reverse(true).Render(msg)
	lw := m.width - ansi.StringWidth(right)
	return fitANSI(left, lw) + right
}

// normalHints lists the most important actions for the current row and
// selection; all others are in the help view.
func (m *Model) normalHints() []string {
	var pairs []string
	if row := m.currentRow(); row.count > 0 {
		action := "expand"
		if m.expanded[row.family] {
			action = "collapse"
		}
		pairs = append(pairs, "Enter", action)
	} else if m.current() != nil {
		pairs = append(pairs, "Enter", "lazygit")
	}
	if len(m.marks) > 0 {
		pairs = append(pairs, "f", "fetch marked", "u", "update marked", "A", "clear marks")
	} else {
		pairs = append(pairs, "f", "fetch", "F", "fetch shown", "u", "update")
	}
	pairs = append(pairs, "/", "filter")
	if m.filter.Text != "" {
		pairs = append(pairs, "Esc", "clear filter")
	}
	return append(pairs, "?", "all keys")
}

// operationHints lists the actions that apply to the batch and the selected
// row of the operations view.
func (m *Model) operationHints() []string {
	pairs := []string{"j/k", "move", "Enter", "lazygit"}
	if b := m.batch; b != nil {
		failed := false
		for _, r := range b.rows {
			failed = failed || r.result.Status == domain.StatusFailed || r.result.Status == domain.StatusCancelled
		}
		switch {
		case !b.done():
			pairs = append(pairs, "c", "cancel")
		case failed:
			pairs = append(pairs, "x", "retry failed")
		}
		if b.done() && b.op == domain.OperationUpdate {
			pairs = append(pairs, "u", "continue")
		}
		if f := m.refetch; f != nil && f.ready && f.batch == b {
			pairs = append(pairs, "u", "update preview")
		}
		if m.overlayIdx < len(b.rows) {
			if row := b.rows[m.overlayIdx]; row.job.Operation == domain.OperationFetch && row.result.Status == domain.StatusFailed {
				pairs = append(pairs, "t", "retry in terminal")
			}
		}
	}
	return append(pairs, "Esc", "close")
}

// modal returns the dialog for the current mode, or nil.
func (m *Model) modal(h int) []string {
	type spec struct {
		width int
		build func(iw, rows int) (string, []string)
	}
	specs := map[mode]spec{
		modeHelp:          {96, m.viewHelp},
		modeError:         {100, m.viewError},
		modeSort:          {40, m.viewSort},
		modeFamily:        {50, m.viewFamily},
		modeOperations:    {100, m.viewOperations},
		modeConfirmFetch:  {72, m.viewConfirmFetch},
		modeUpdatePreview: {100, m.viewUpdatePreview},
		modeLaunchers:     {50, m.viewLaunchers},
		modeConfirmQuit:   {72, m.viewConfirmQuit},
	}
	s, ok := specs[m.mode]
	if !ok {
		return nil
	}
	mw := min(s.width, m.width-4)
	rows := max(h-4, 3)
	title, lines := s.build(mw-4, rows)
	if len(lines) > rows {
		lines = lines[:rows]
	}
	padded := make([]string, len(lines))
	for i, l := range lines {
		padded[i] = " " + fitANSI(l, mw-4) + " "
	}
	return m.box(title, "", padded, mw, len(padded)+2, true)
}

// window returns at most rows lines around the selected index.
func window(lines []string, selected, rows int) []string {
	if len(lines) <= rows || rows <= 0 {
		return lines
	}
	start := windowStart(len(lines), selected, rows)
	return lines[start : start+rows]
}

// windowStart is the first of rows lines out of n that keep selected near
// the middle; n must be greater than rows.
func windowStart(n, selected, rows int) int {
	return min(max(selected-rows/2, 0), n-rows)
}

// selectable renders a plain text line of a list, highlighted if selected.
func (m *Model) selectable(text string, selected bool, iw int, style lipgloss.Style) string {
	text = domain.Printable(text)
	if selected {
		return m.theme.selected.Render(fitANSI(text, iw))
	}
	return style.Render(text)
}

func (m *Model) viewHelp(iw, rows int) (string, []string) {
	col := func(groups []BindingGroup) []string {
		var out []string
		for _, g := range groups {
			out = append(out, m.theme.panelTitle.Render(g.Title))
			for _, b := range g.Bindings {
				out = append(out, m.theme.key.Render(fmt.Sprintf("  %-12s", b.Keys))+" "+b.Action)
			}
			out = append(out, "")
		}
		return out
	}
	legend := BindingGroup{Title: "Symbols", Bindings: m.icons.legend()}
	var left, right []string
	if iw >= 84 {
		left = col(Bindings[:3])
		right = col(append(append([]BindingGroup{}, Bindings[3:]...), legend))
		w := iw / 2
		var out []string
		for i := 0; i < max(len(left), len(right)); i++ {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			out = append(out, fitANSI(l, w-2)+"  "+r)
		}
		return "Help", m.scroll(out, rows)
	}
	return "Help", m.scroll(col(append(append([]BindingGroup{}, Bindings...), legend)), rows)
}

// scroll applies the help scroll offset.
func (m *Model) scroll(lines []string, rows int) []string {
	maxOffset := max(len(lines)-rows, 0)
	m.overlayIdx = min(m.overlayIdx, maxOffset)
	return lines[m.overlayIdx:]
}

// renderLines splits external multi-line text, such as Git error output, into
// printable lines with a prefix.
func renderLines(style lipgloss.Style, prefix, text string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		out = append(out, style.Render(prefix+domain.Printable(l)))
	}
	return out
}

func (m *Model) viewError(iw, rows int) (string, []string) {
	t := m.theme
	var body []string
	title := "Errors and warnings"
	if r := m.current(); r != nil {
		title = r.Name + " ─ errors and warnings"
		found := false
		if r.EntryError != nil {
			body = append(body, renderLines(t.err, "", "entry: "+r.Availability.String()+": "+r.EntryError.Error())...)
			found = true
		}
		if r.Inspection.Err != nil {
			body = append(body, renderLines(t.err, "", "inspection: "+r.Inspection.Err.Error())...)
			found = true
		}
		if last := m.lastOp(r.ID); last != nil {
			l := fmt.Sprintf("last %s: %s", last.Operation, last.Status)
			if last.ExitCode != nil {
				l += fmt.Sprintf(" (exit status %d)", *last.ExitCode)
			}
			if last.Error != nil {
				l += ": " + firstLine(last.Error.Error())
			}
			style := t.success
			if last.Status == domain.StatusFailed {
				style = t.err
			}
			body = append(body, style.Render(domain.Printable(l)))
			if s := strings.TrimSpace(last.Stderr); s != "" {
				body = append(body, renderLines(t.dim, "  ", s)...)
			}
			found = true
		}
		if !found {
			body = append(body, t.success.Render(m.icons.clean+" no errors"))
		}
	}
	if len(m.warnings) > 0 {
		body = append(body, "", t.panelTitle.Render("Warnings"))
		for _, w := range m.warnings {
			body = append(body, renderLines(t.warn, "  ", w)...)
		}
	}
	return title, body
}

func (m *Model) viewSort(iw, rows int) (string, []string) {
	var body []string
	for i, k := range app.SortKeys {
		text := "  " + k.String()
		if k == m.sortKey {
			text = m.icons.clean + " " + k.String()
		}
		body = append(body, m.selectable(text, i == m.overlayIdx, iw, m.theme.plain))
	}
	return "Sort by", body
}

func (m *Model) viewFamily(iw, rows int) (string, []string) {
	head := "Search: " + domain.Printable(m.familyQuery) + m.theme.key.Render("█")
	var body []string
	for i, choice := range m.familyChoices() {
		prefix := "  "
		if choice == m.familyIdx {
			prefix = m.icons.clean + " "
		}
		body = append(body, m.selectable(prefix+m.familyLabel(choice), i == m.overlayIdx, iw, m.theme.plain))
	}
	if len(body) == 0 {
		body = append(body, m.theme.dim.Render("No matching families"))
	}
	return "Choose family", append([]string{head}, window(body, m.overlayIdx, rows-1)...)
}

// opGroups orders the rows of the operations view by status: rows that need
// attention first, completed rows last.
var opGroups = []struct {
	title  string
	status domain.OperationStatus
}{
	{"running", domain.StatusRunning},
	{"failed", domain.StatusFailed},
	{"cancelled", domain.StatusCancelled},
	{"skipped", domain.StatusSkipped},
	{"queued", domain.StatusQueued},
	{"completed", domain.StatusSucceeded},
}

// opOrder returns the indexes of the batch rows in display order.
func (m *Model) opOrder() []int {
	if m.batch == nil {
		return nil
	}
	var order []int
	for _, g := range opGroups {
		for i, row := range m.batch.rows {
			if row.result.Status == g.status {
				order = append(order, i)
			}
		}
	}
	return order
}

func (m *Model) firstOpRow() int {
	if order := m.opOrder(); len(order) > 0 {
		return order[0]
	}
	return 0
}

func (m *Model) viewOperations(iw, rows int) (string, []string) {
	if m.batch == nil {
		return "Operations", []string{"no operations"}
	}
	t := m.theme
	counts := map[domain.OperationStatus]int{}
	for _, r := range m.batch.rows {
		counts[r.result.Status]++
	}
	total := len(m.batch.rows)
	done := counts[domain.StatusSucceeded] + counts[domain.StatusFailed] + counts[domain.StatusSkipped] + counts[domain.StatusCancelled]
	summary := fmt.Sprintf(" %3d%% · %d/%d · %d ok · %d failed · %d skipped", done*100/max(total, 1), done, total,
		counts[domain.StatusSucceeded], counts[domain.StatusFailed], counts[domain.StatusSkipped])
	bar := m.progressBar(done, total, max(iw-ansi.StringWidth(summary), 10))
	head := []string{bar + summary}
	if f := m.refetch; f != nil && f.batch == m.batch {
		note := "Press u for the update preview once the fetch is finished."
		if f.ready {
			note = "Fetch finished. Press u for the update preview."
		}
		head = append(head, t.dim.Render(note))
	}

	var list []string
	// group holds for every line the index of its group title; the blank
	// line before a title belongs to the title.
	var group []int
	selLine := 0
	for _, g := range opGroups {
		if counts[g.status] == 0 {
			continue
		}
		icon, style := m.icons.queued, t.dim
		switch g.status {
		case domain.StatusRunning:
			icon, style = m.spinnerFrame(), t.key
		case domain.StatusSucceeded:
			icon, style = m.icons.clean, t.success
		case domain.StatusFailed:
			icon, style = m.icons.failed, t.err
		case domain.StatusSkipped:
			icon, style = "–", t.warn
		case domain.StatusCancelled:
			icon = "–"
		}
		title := len(list) + 1
		list = append(list, "", t.header.Render(fmt.Sprintf("%s %d", strings.ToUpper(g.title), counts[g.status])))
		group = append(group, title, title)
		for i, row := range m.batch.rows {
			if row.result.Status != g.status {
				continue
			}
			group = append(group, title)
			name := string(row.job.RepositoryID)
			if r := m.repo(row.job.RepositoryID); r != nil {
				name = r.Name
			}
			text := fmt.Sprintf(" %s %s", icon, runewidth.FillRight(runewidth.Truncate(domain.Printable(name), 24, "…"), 24))
			if row.result.Status.Final() && row.result.Error != nil {
				text += " " + firstLine(row.result.Error.Error())
			}
			selected := i == m.overlayIdx
			if selected {
				selLine = len(list)
			}
			if fu, resolved := m.followUp(row); fu != "" {
				fuStyle := t.warn
				if resolved {
					fuStyle = t.success
				}
				if selected {
					list = append(list, m.selectable(text+"  → "+fu, true, iw, style))
				} else {
					list = append(list, style.Render(domain.Printable(text))+fuStyle.Render("  → "+fu))
				}
				continue
			}
			list = append(list, m.selectable(text, selected, iw, style))
		}
	}

	var detail []string
	if m.overlayIdx < total {
		res := m.batch.rows[m.overlayIdx].result
		if s := strings.TrimSpace(res.Stderr); s != "" && res.Status == domain.StatusFailed {
			detail = append(detail, "", t.panelTitle.Render("stderr"))
			detail = append(detail, renderLines(t.dim, "  ", s)...)
			if len(detail) > 8 {
				detail = append(detail[:8], t.dim.Render("  …"))
			}
		}
	}
	state := "running"
	if m.batch.done() {
		state = "finished"
	}
	body := append(head, operationWindow(list, group, selLine, rows-len(head)-len(detail))...)
	return fmt.Sprintf("%s ─ %d repositories ─ %s", m.batch.op, total, state), append(body, detail...)
}

// operationWindow returns at most rows lines around the selected line. If the
// title of the first shown group is scrolled off, it is kept as the first
// line, because the rows carry their status only through their group.
func operationWindow(list []string, group []int, selected, rows int) []string {
	if len(list) <= rows || rows < 2 {
		return window(list, selected, rows)
	}
	start := windowStart(len(list), selected, rows)
	if group[start] >= start {
		return list[start : start+rows]
	}
	start = windowStart(len(list), selected, rows-1)
	if group[start] >= start {
		return list[start:min(start+rows, len(list))]
	}
	return append([]string{list[group[start]]}, list[start:start+rows-1]...)
}

func (m *Model) viewConfirmFetch(iw, rows int) (string, []string) {
	t := m.theme
	stores := app.StoreCount(m.fetchTargets)
	unavailable := 0
	for _, r := range m.fetchTargets {
		if !r.IsAvailable() {
			unavailable++
		}
	}
	body := []string{
		t.bold.Render(fmt.Sprintf("Fetch %d repositories (%d repository stores)?", len(m.fetchTargets)-unavailable, stores)),
		"",
	}
	if m.fetchHidden > 0 {
		body = append(body, t.warn.Render(m.hiddenMarksNotice()))
	}
	if unavailable > 0 {
		body = append(body, t.dim.Render(fmt.Sprintf("%d unavailable entries are skipped.", unavailable)))
	}
	cmd := "git fetch --all"
	if m.app.Config.Git.FetchPrune {
		cmd += " --prune"
	}
	body = append(body, t.dim.Render("Command: ")+cmd, "", m.hints("Enter/y", "start", "Esc/n", "cancel"))
	return "Confirm fetch", body
}

func (m *Model) viewConfirmQuit(iw, rows int) (string, []string) {
	t := m.theme
	open := 0
	for _, r := range m.batch.rows {
		if !r.result.Status.Final() {
			open++
		}
	}
	return "Quit", []string{
		t.bold.Render(fmt.Sprintf("The %s batch has %d unfinished repositories.", m.batch.op, open)),
		"",
		"Quitting cancels them. Running Git processes are asked to stop,",
		"so that they can remove their lock files.",
		"",
		m.hints("Enter/y", "quit", "Esc/n", "keep running"),
	}
}

func (m *Model) viewUpdatePreview(iw, rows int) (string, []string) {
	t := m.theme
	now := m.now()
	var list []string
	idx := 0
	for _, c := range m.updateOK {
		r := c.Repository
		text := fmt.Sprintf("%s update  %-24s %s%d · %s", m.icons.clean, ansi.Truncate(r.Name, 24, "…"),
			m.icons.behind, r.Upstream.Value.Behind, m.fetchSeg(now, &r).text)
		list = append(list, m.selectable(text, idx == m.overlayIdx, iw, t.success))
		idx++
	}
	for _, c := range m.updateSkip {
		r := c.Repository
		text := fmt.Sprintf("– skip    %-24s %s", ansi.Truncate(r.Name, 24, "…"), c.SkipReason)
		list = append(list, m.selectable(text, idx == m.overlayIdx, iw, t.dim))
		idx++
	}
	head := []string{
		t.bold.Render(fmt.Sprintf("%d repositories will be fast-forwarded, %d skipped.", len(m.updateOK), len(m.updateSkip))),
		t.dim.Render("git merge --ff-only --no-autostash @{upstream} · no network access; press f to fetch first"),
	}
	if m.fetchHidden > 0 {
		head = append(head, t.warn.Render(m.hiddenMarksNotice()))
	}
	head = append(head, "")
	return "Safe update preview", append(head, window(list, m.overlayIdx, rows-len(head))...)
}

func (m *Model) hiddenMarksNotice() string {
	if m.tree {
		return fmt.Sprintf("Includes %d marked repos hidden by filters or collapsed groups.", m.fetchHidden)
	}
	return fmt.Sprintf("Includes %d marked repositories hidden by the filter.", m.fetchHidden)
}

func (m *Model) viewLaunchers(iw, rows int) (string, []string) {
	var body []string
	for i, it := range m.launcherItems() {
		body = append(body, m.selectable("  "+it.label, i == m.overlayIdx, iw, m.theme.plain))
	}
	title := "Launch"
	if r := m.current(); r != nil {
		title += " ─ " + r.Name
	}
	return title, body
}
