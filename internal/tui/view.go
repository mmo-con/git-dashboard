package tui

import (
	"fmt"
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
	colHealth
	colFamily
	colBranch
	colStatus
	colUpstream
	colCommit
)

var colSpecs = []colSpec{
	colMark: {"", 2, 2, 2}, colName: {"Repository", 6, 10, 40}, colHealth: {"", 1, 1, 1},
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
	free := iw - (len(colSpecs) - 1)
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
		{colStatus, 12}, {colFamily, 6}, {colBranch, 8}, {colName, 10}, {colCommit, 3}, {colUpstream, 8}, {colStatus, 8},
		{colFamily, 4}, {colBranch, 6}, {colName, 6}, {colUpstream, 6}, {colStatus, 6},
	} {
		for free < 0 && cols[s.col].width > max(s.floor, colSpecs[s.col].min) {
			cols[s.col].width--
			free++
		}
	}
	return cols
}

// rowCells returns the cells of r without the cursor and mark column. The
// names of healthy repositories are not emphasized, so rows that need
// attention stand out.
func (m *Model) rowCells(r *domain.Repository, now time.Time) [][]seg {
	age := seg{"", m.theme.dim}
	if r.LastCommit.Valid && r.LastCommit.Value != nil {
		age.text = shortAgo(now, r.LastCommit.Value.Time)
	}
	health, healthy := m.health(r)
	name := m.theme.bold
	if healthy {
		name = m.theme.plain
	}
	return [][]seg{
		{{r.Name, name}},
		{health},
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
			cells[i][0] = append([]seg{{"  ", m.theme.dim}}, cells[i][0]...)
		}
		for j, c := range cells[i] {
			natural[j+colName] = max(natural[j+colName], runewidth.StringWidth(plain(c)))
		}
	}
	cols := columns(iw, natural)
	var lines []string
	var header []string
	for _, c := range cols {
		header = append(header, fitANSI(c.title, c.width))
	}
	lines = append(lines, m.theme.header.Render(strings.Join(header, " ")))

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
			text := fmt.Sprintf("%s  %s %s (%d)", cursor, toggle, domain.Printable(string(entry.family)), entry.count)
			lines = append(lines, base.Render(fitANSI(text, iw)))
			continue
		}
		mark := seg{" ", m.theme.plain}
		if m.marks[entry.id] {
			mark = seg{m.icons.mark, m.theme.key}
		}
		row := append([][]seg{{{cursor, m.theme.accent}, mark}}, cells[i]...)
		if i == m.cursor {
			for _, cell := range row {
				for k := range cell {
					cell[k].style = m.theme.onSelected(cell[k].style)
				}
			}
		}
		var parts []string
		for j, c := range cols {
			parts = append(parts, renderCell(row[j], c.width, base))
		}
		parts = append(parts, base.Render(strings.Repeat(" ", max(iw-tableWidth(cols), 0))))
		lines = append(lines, strings.Join(parts, base.Render(" ")))
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

func tableWidth(cols []column) int {
	w := len(cols)
	for _, c := range cols {
		w += c.width
	}
	return w
}

func (m *Model) emptyText() string {
	switch {
	case m.loadingInventory:
		return " " + m.spinnerFrame() + " loading inventory…"
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
		return m.box(string(row.family), "", []string{
			fmt.Sprintf(" Matching repositories: %d", row.count),
			" Enter: " + action + " family",
		}, w, h, false)
	}
	r := m.current()
	if r == nil {
		return m.box("Details", "", nil, w, h, false)
	}
	lines := m.detailLines(r).fit(h - 2)
	for i := range lines {
		lines[i] = " " + lines[i]
	}
	return m.box(r.Name, string(r.Source), lines, w, h, false)
}

type detailKind int

const (
	detailText detailKind = iota
	detailBlank
	detailSection
)

// details collects the lines of the detail panel, grouped into sections.
type details struct {
	lines []string
	kinds []detailKind
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

// fit returns at most n lines. If the panel is too small, blank lines are
// dropped first, then the section titles.
func (d *details) fit(n int) []string {
	keep := func(drop ...detailKind) []string {
		var out []string
		for i, l := range d.lines {
			if !slices.Contains(drop, d.kinds[i]) {
				out = append(out, l)
			}
		}
		return out
	}
	switch {
	case len(d.lines) <= n:
		return keep()
	case len(keep(detailBlank)) <= n:
		return keep(detailBlank)
	}
	return keep(detailBlank, detailSection)
}

// detailLabelWidth aligns the labels of the detail panel.
const detailLabelWidth = 10

func (m *Model) label(s string) seg {
	return seg{runewidth.FillRight(s, detailLabelWidth), m.theme.dim}
}

func (m *Model) detailLines(r *domain.Repository) *details {
	t := m.theme
	now := m.now()
	d := &details{}
	d.text(seg{r.Path, t.dim})
	d.text(m.label("family"), seg{string(r.Family), t.key})
	if r.Workspace != "" && r.Workspace != r.Path {
		d.text(m.label("workspace"), seg{r.Workspace, t.dim})
	}

	if r.IsAvailable() && loaded(r) {
		d.section("branch", t.header)
		b := append([]seg{}, m.branchSegs(r)...)
		if r.Upstream.Valid && r.Upstream.Value.Configured {
			b = append(b, seg{" → " + r.Upstream.Value.Ref, t.dim})
		}
		b = append(b, seg{"  ", t.plain})
		d.text(append(b, m.upstreamSegs(r)...)...)

		if bs := m.otherBranchLines(r); len(bs) > 0 {
			d.add("", detailBlank)
			d.add(bs[0], detailSection)
			for _, l := range bs[1:] {
				d.add(l, detailText)
			}
		}

		if r.Worktree.Valid {
			d.section("worktree", t.header)
			if r.Operation.Valid && r.Operation.Value != domain.NoOperation {
				d.text(seg{r.Operation.Value.String() + " in progress", t.err.Bold(true)})
			}
			d.text(m.worktreeSegs(r.Worktree.Value)...)
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

	if e := m.firstError(r); e != "" {
		d.section("error", t.err.Bold(true))
		d.text(seg{m.icons.failed + " " + firstLine(e), t.err})
		d.text(seg{"press e for details", t.dim})
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
		{ic.staged, "staged", w.Staged, t.ok},
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
		pairs := []string{"j/k", "move", "Enter", "lazygit", "c", "cancel", "x", "retry failed"}
		if m.batch != nil && m.batch.op == domain.OperationUpdate {
			pairs = append(pairs, "u", "continue")
		} else {
			pairs = append(pairs, "t", "retry in terminal")
		}
		left = " " + m.hints(append(pairs, "Esc", "close")...)
	case modeConfirmFetch, modeUpdatePreview:
		left = " " + m.hints("Enter/y", "confirm", "Esc/n", "cancel")
	case modeConfirmQuit:
		left = " " + m.hints("Enter/y", "quit", "Esc/n", "keep running")
	case modeSort, modeLaunchers:
		left = " " + m.hints("j/k", "move", "Enter", "choose", "Esc", "close")
	case modeError:
		left = " " + m.hints("t", "fetch in terminal", "Esc", "close")
	case modeHelp:
		left = " " + m.hints("j/k", "scroll", "any other key", "close")
	default:
		enter := "lazygit"
		if m.currentRow().count > 0 {
			enter = "toggle family"
		}
		left = " " + m.hints("Enter", enter, "v", "tree/list", "f", "fetch", "F", "fetch shown", "u", "update",
			"Space", "mark", "/", "filter", "t", "families", "?", "help", "q", "quit")
	}
	if m.message == "" {
		return fitANSI(left, m.width)
	}
	style, icon := m.theme.ok, m.icons.clean
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
	start := max(selected-rows/2, 0)
	if start+rows > len(lines) {
		start = len(lines) - rows
	}
	return lines[start : start+rows]
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
			style := t.ok
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
			body = append(body, t.ok.Render(m.icons.clean+" no errors"))
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

func (m *Model) viewOperations(iw, rows int) (string, []string) {
	if m.batch == nil {
		return "Operations", []string{"no operations"}
	}
	t := m.theme
	counts := map[domain.OperationStatus]int{}
	for _, r := range m.batch.rows {
		counts[r.result.Status]++
	}
	done := counts[domain.StatusSucceeded] + counts[domain.StatusFailed] + counts[domain.StatusSkipped] + counts[domain.StatusCancelled]
	summary := fmt.Sprintf(" %d/%d · %d ok · %d failed · %d skipped", done, len(m.batch.rows),
		counts[domain.StatusSucceeded], counts[domain.StatusFailed], counts[domain.StatusSkipped])
	bar := m.progressBar(done, len(m.batch.rows), max(iw-ansi.StringWidth(summary), 10))
	head := []string{bar + summary, ""}

	var rowsOut []string
	for i, row := range m.batch.rows {
		name := string(row.job.RepositoryID)
		if r := m.repo(row.job.RepositoryID); r != nil {
			name = r.Name
		}
		icon, style := m.icons.queued, t.dim
		switch row.result.Status {
		case domain.StatusRunning:
			icon, style = m.spinnerFrame(), t.key
		case domain.StatusSucceeded:
			icon, style = m.icons.clean, t.ok
		case domain.StatusFailed:
			icon, style = m.icons.failed, t.err
		case domain.StatusSkipped:
			icon, style = "–", t.warn
		case domain.StatusCancelled:
			icon, style = "–", t.dim
		}
		status := string(row.result.Status)
		if row.result.Status.Final() && row.result.Error != nil {
			status += ": " + firstLine(row.result.Error.Error())
		}
		text := domain.Printable(fmt.Sprintf("%s %-24s %s", icon, ansi.Truncate(name, 24, "…"), status))
		if fu, resolved := m.followUp(row); fu != "" {
			fuStyle := t.warn
			if resolved {
				fuStyle = t.ok
			}
			if i == m.overlayIdx {
				rowsOut = append(rowsOut, m.selectable(text+"  → "+fu, true, iw, style))
			} else {
				rowsOut = append(rowsOut, style.Render(text)+fuStyle.Render("  → "+fu))
			}
			continue
		}
		rowsOut = append(rowsOut, m.selectable(text, i == m.overlayIdx, iw, style))
	}

	var detail []string
	if m.overlayIdx < len(m.batch.rows) {
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
	body := append(head, window(rowsOut, m.overlayIdx, rows-len(head)-len(detail))...)
	return fmt.Sprintf("%s ─ %d repositories ─ %s", m.batch.op, len(m.batch.rows), state), append(body, detail...)
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
		list = append(list, m.selectable(text, idx == m.overlayIdx, iw, t.ok))
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
		t.dim.Render("git merge --ff-only --no-autostash @{upstream} · no network access; fetch first with F"),
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
