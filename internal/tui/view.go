package tui

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/mmo-con/git-dashboard/internal/app"
	"github.com/mmo-con/git-dashboard/internal/domain"
)

const detailHeight = 7

func (m *Model) tableHeight() int {
	// title, table header, detail pane, message line, footer
	return max(m.height-1-1-detailHeight-1-1, 3)
}

func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return runewidth.FillRight(runewidth.Truncate(s, w, "…"), w)
}

func (m *Model) View() string {
	var b strings.Builder
	b.WriteString(m.titleLine())
	b.WriteByte('\n')
	switch m.mode {
	case modeHelp:
		b.WriteString(m.viewHelp())
	case modeError:
		b.WriteString(m.viewError())
	case modeSort:
		b.WriteString(m.viewSort())
	case modeOperations:
		b.WriteString(m.viewOperations())
	case modeConfirmFetch:
		b.WriteString(m.viewConfirmFetch())
	case modeUpdatePreview:
		b.WriteString(m.viewUpdatePreview())
	case modeLaunchers:
		b.WriteString(m.viewLaunchers())
	default:
		b.WriteString(m.viewTable())
		b.WriteByte('\n')
		b.WriteString(m.viewDetails())
	}
	b.WriteByte('\n')
	b.WriteString(m.messageLine())
	b.WriteByte('\n')
	b.WriteString(m.footer())
	return b.String()
}

func (m *Model) titleLine() string {
	family := "All"
	if m.filter.Family != "" {
		family = string(m.filter.Family)
	}
	parts := []string{"Git Dashboard", "Family: " + family}
	if m.filter.Text != "" || m.mode == modeFilter {
		parts = append(parts, "Filter: "+m.filter.Text)
	}
	parts = append(parts, "Sort: "+m.sortKey.String(),
		fmt.Sprintf("%d/%d", len(m.visible), len(m.repos)),
		fmt.Sprintf("Marked: %d", len(m.marks)))
	if m.loadingInventory {
		parts = append(parts, "loading inventory…")
	} else if m.pendingInspect > 0 {
		parts = append(parts, fmt.Sprintf("inspecting %d…", m.pendingInspect))
	}
	if m.batch != nil && !m.batch.done() {
		done := 0
		for _, r := range m.batch.rows {
			if r.result.Status.Final() {
				done++
			}
		}
		parts = append(parts, fmt.Sprintf("%s %d/%d", m.batch.op, done, len(m.batch.rows)))
	}
	return styleTitle.Render(fit(" "+strings.Join(parts, " ─ "), m.width))
}

type column struct {
	title string
	width int
}

func (m *Model) columns() []column {
	cols := []column{
		{"", 2}, {"Repository", 0}, {"Family", 12}, {"Branch", 16},
		{"Status", 26}, {"Upstream", 16}, {"Commit", 6},
	}
	fixed := 0
	for _, c := range cols {
		fixed += c.width + 1
	}
	name := m.width - fixed
	for name < 12 && cols[4].width > 12 {
		cols[4].width--
		name++
	}
	for name < 12 && cols[3].width > 8 {
		cols[3].width--
		name++
	}
	for name < 12 && cols[2].width > 6 {
		cols[2].width--
		name++
	}
	cols[1].width = max(name, 8)
	return cols
}

func (m *Model) viewTable() string {
	cols := m.columns()
	h := m.tableHeight()
	var lines []string
	var header []string
	for _, c := range cols {
		header = append(header, fit(c.title, c.width))
	}
	lines = append(lines, styleHeader.Render(fit(strings.Join(header, " "), m.width)))

	rows := h - 1
	if len(m.visible) == 0 {
		lines = append(lines, styleDim.Render(m.emptyText()))
	}
	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	now := m.now()
	for i := start; i < len(m.visible) && i < start+rows; i++ {
		r := m.repo(m.visible[i])
		last := m.lastOp(r.ID)
		mark := " "
		if m.marks[r.ID] {
			mark = "●"
		}
		age := ""
		if r.LastCommit.Valid && r.LastCommit.Value != nil {
			age = shortAgo(now, r.LastCommit.Value.Time)
		}
		cells := []string{mark, r.Name, string(r.Family), branchText(r), statusText(r), upstreamText(r, last), age}
		var parts []string
		for j, c := range cols {
			parts = append(parts, fit(cells[j], c.width))
		}
		line := fit(strings.Join(parts, " "), m.width)
		switch {
		case i == m.cursor:
			line = styleSelected.Render(line)
		case severity(r, last) == 2:
			line = styleError.Render(line)
		case severity(r, last) == 1:
			line = styleWarn.Render(line)
		}
		lines = append(lines, line)
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) emptyText() string {
	switch {
	case m.loadingInventory:
		return "  loading inventory…"
	case len(m.repos) == 0 && m.app.Config.File == "":
		return fmt.Sprintf("  no configuration found at %s; see examples/config.toml", m.app.ConfigPath)
	case len(m.repos) == 0:
		return "  the configuration contains no repositories"
	}
	return "  no repository matches the current filter (Esc clears the filter, Tab switches family)"
}

func (m *Model) viewDetails() string {
	var lines []string
	sep := styleDim.Render(strings.Repeat("─", max(m.width, 1)))
	lines = append(lines, sep)
	r := m.current()
	if r == nil {
		for len(lines) < detailHeight {
			lines = append(lines, "")
		}
		return strings.Join(lines, "\n")
	}
	now := m.now()
	head := fmt.Sprintf("%s · %s · %s", r.Name, r.Family, r.Source)
	if others := m.worktreeSiblings(r); len(others) > 0 {
		head += fmt.Sprintf(" · worktrees: %s", strings.Join(others, ", "))
	}
	lines = append(lines, styleKey.Render(fit(head, m.width)), fit(r.Path, m.width))

	if r.Remotes.Valid {
		var rem []string
		for _, x := range r.Remotes.Value {
			rem = append(rem, x.Name+": "+domain.RedactURL(x.URL))
		}
		if len(rem) == 0 {
			rem = append(rem, "no remotes")
		}
		line := strings.Join(rem, " · ")
		if r.RemoteMismatch() {
			line = fmt.Sprintf("expected %s: %s (mismatch) · %s", remoteName(r.ExpectedRemote), domain.RedactURL(r.ExpectedRemote.URL), line)
			lines = append(lines, styleWarn.Render(fit(line, m.width)))
		} else {
			lines = append(lines, fit(line, m.width))
		}
	} else {
		lines = append(lines, "")
	}

	commit := ""
	if r.LastCommit.Valid {
		if c := r.LastCommit.Value; c != nil {
			commit = fmt.Sprintf("last: %s %s · %s · %s", c.ShortID, c.Subject, c.Author, ago(now, c.Time))
		} else {
			commit = "no commits yet"
		}
	}
	if r.IsAvailable() {
		commit += " · " + fetchAge(now, r)
	}
	lines = append(lines, fit(commit, m.width))

	if r.Worktree.Valid {
		w := r.Worktree.Value
		untracked := fmt.Sprintf("untracked %d", w.Untracked)
		if !w.UntrackedChecked {
			untracked = "untracked not checked"
		}
		line := fmt.Sprintf("staged %d · modified %d · deleted %d · renamed %d · conflicts %d · %s",
			w.Staged, w.Modified, w.Deleted, w.Renamed, w.Conflicts, untracked)
		if r.Operation.Valid && r.Operation.Value != domain.NoOperation {
			line = r.Operation.Value.String() + " in progress · " + line
		}
		lines = append(lines, fit(line, m.width))
	} else {
		lines = append(lines, "")
	}

	if e := m.firstError(r); e != "" {
		lines = append(lines, styleError.Render(fit(firstLine(e)+"  (e: details)", m.width)))
	}
	for len(lines) < detailHeight {
		lines = append(lines, "")
	}
	return strings.Join(lines[:detailHeight], "\n")
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
		return r.Name + ": " + r.EntryError.Error()
	case r.Inspection.Err != nil:
		return r.Name + ": " + r.Inspection.Err.Error()
	}
	if last := m.lastOp(r.ID); last != nil && last.Error != nil {
		return fmt.Sprintf("%s: %s %s: %v", r.Name, last.Operation, last.Status, last.Error)
	}
	return ""
}

func (m *Model) messageLine() string {
	if m.mode == modeFilter {
		return fit("/"+m.filter.Text+"█", m.width)
	}
	if m.message == "" {
		return ""
	}
	if m.messageErr {
		return styleError.Render(fit(m.message, m.width))
	}
	return styleOK.Render(fit(m.message, m.width))
}

func (m *Model) footer() string {
	var hints []string
	switch m.mode {
	case modeFilter:
		hints = []string{"Enter apply", "Esc clear", "Ctrl+U erase", "is:dirty is:behind is:ahead is:error …"}
	case modeOperations:
		hints = []string{"j/k move", "c cancel", "x retry failed", "t retry in terminal", "Esc close"}
	case modeConfirmFetch, modeUpdatePreview:
		hints = []string{"Enter/y confirm", "Esc/n cancel"}
	case modeSort, modeLaunchers:
		hints = []string{"j/k move", "Enter choose", "Esc close"}
	case modeError:
		hints = []string{"t fetch in terminal", "Esc close"}
	case modeHelp:
		hints = []string{"any key closes help"}
	default:
		hints = []string{"Enter lazygit", "f fetch", "F fetch visible", "u update", "Space mark", "/ filter", "Tab family", "? help", "q quit"}
	}
	return styleDim.Render(fit(" "+strings.Join(hints, "  "), m.width))
}

// overlay renders content in the area of the table and the detail pane.
func (m *Model) overlay(title string, body []string) string {
	h := m.tableHeight() + detailHeight + 1
	lines := []string{styleKey.Render(title), ""}
	lines = append(lines, body...)
	if len(lines) > h {
		lines = append(lines[:h-1], styleDim.Render("…"))
	}
	for i := range lines {
		lines[i] = fit(lines[i], m.width)
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// window returns the lines around the selected index that fit the overlay.
func (m *Model) window(lines []string, selected, reserved int) []string {
	h := max(m.tableHeight()+detailHeight+1-2-reserved, 1)
	if len(lines) <= h {
		return lines
	}
	start := max(selected-h/2, 0)
	if start+h > len(lines) {
		start = len(lines) - h
	}
	return lines[start : start+h]
}

func (m *Model) viewHelp() string {
	var body []string
	for _, g := range Bindings {
		body = append(body, styleHeader.Render(g.Title))
		for _, b := range g.Bindings {
			body = append(body, fmt.Sprintf("  %-18s %s", b.Keys, b.Action))
		}
		body = append(body, "")
	}
	body = append(body, "Filter keywords: "+strings.Join(app.StateKeywords, " "))
	return m.overlay("Help", body)
}

func (m *Model) viewError() string {
	var body []string
	if r := m.current(); r != nil {
		body = append(body, styleHeader.Render(r.Name+" — "+r.Path))
		found := false
		if r.EntryError != nil {
			body = append(body, "entry: "+r.Availability.String()+": "+r.EntryError.Error())
			found = true
		}
		if r.Inspection.Err != nil {
			body = append(body, strings.Split("inspection: "+r.Inspection.Err.Error(), "\n")...)
			found = true
		}
		if last := m.lastOp(r.ID); last != nil {
			line := fmt.Sprintf("last %s: %s", last.Operation, last.Status)
			if last.ExitCode != nil {
				line += fmt.Sprintf(" (exit status %d)", *last.ExitCode)
			}
			if last.Error != nil {
				line += ": " + firstLine(last.Error.Error())
			}
			body = append(body, line)
			if s := strings.TrimSpace(last.Stderr); s != "" {
				for _, l := range strings.Split(s, "\n") {
					body = append(body, "  "+l)
				}
			}
			found = true
		}
		if !found {
			body = append(body, "no errors")
		}
		body = append(body, "")
	}
	if len(m.warnings) > 0 {
		body = append(body, styleHeader.Render("Warnings"))
		for _, w := range m.warnings {
			for _, l := range strings.Split(w, "\n") {
				body = append(body, styleWarn.Render("  "+l))
			}
		}
	}
	return m.overlay("Errors and warnings", body)
}

func (m *Model) viewSort() string {
	var body []string
	for i, k := range app.SortKeys {
		line := "  " + k.String()
		if k == m.sortKey {
			line += "  (current)"
		}
		if i == m.overlayIdx {
			line = styleSelected.Render(line)
		}
		body = append(body, line)
	}
	return m.overlay("Sort by", body)
}

func (m *Model) viewOperations() string {
	if m.batch == nil {
		return m.overlay("Operations", []string{"no operations"})
	}
	var rows []string
	for i, row := range m.batch.rows {
		name := string(row.job.RepositoryID)
		if r := m.repo(row.job.RepositoryID); r != nil {
			name = r.Name
		}
		status := string(row.result.Status)
		if row.result.Status.Final() && row.result.Error != nil {
			status += ": " + firstLine(row.result.Error.Error())
		}
		line := fmt.Sprintf("  %-24s %s", runewidth.Truncate(name, 24, "…"), status)
		switch {
		case i == m.overlayIdx:
			line = styleSelected.Render(fit(line, m.width))
		case row.result.Status == domain.StatusFailed:
			line = styleError.Render(line)
		case row.result.Status == domain.StatusSucceeded:
			line = styleOK.Render(line)
		case row.result.Status == domain.StatusSkipped:
			line = styleWarn.Render(line)
		}
		rows = append(rows, line)
	}
	var detail []string
	if m.overlayIdx < len(m.batch.rows) {
		res := m.batch.rows[m.overlayIdx].result
		if s := strings.TrimSpace(res.Stderr); s != "" && res.Status == domain.StatusFailed {
			detail = append(detail, "", styleHeader.Render("stderr"))
			for _, l := range strings.Split(s, "\n") {
				detail = append(detail, "  "+l)
			}
			if len(detail) > 8 {
				detail = append(detail[:8], "  …")
			}
		}
	}
	state := "running"
	if m.batch.done() {
		state = "finished"
	}
	title := fmt.Sprintf("%s — %d repositories — %s", m.batch.op, len(m.batch.rows), state)
	body := append(m.window(rows, m.overlayIdx, len(detail)), detail...)
	return m.overlay(title, body)
}

func (m *Model) viewConfirmFetch() string {
	stores := app.StoreCount(m.fetchTargets)
	unavailable := 0
	for _, r := range m.fetchTargets {
		if !r.IsAvailable() {
			unavailable++
		}
	}
	body := []string{
		fmt.Sprintf("Fetch %d repositories (%d repository stores)?", len(m.fetchTargets)-unavailable, stores),
	}
	if m.fetchHidden > 0 {
		body = append(body, styleWarn.Render(fmt.Sprintf("%d marked repositories are hidden by the current filter and are included.", m.fetchHidden)))
	}
	if unavailable > 0 {
		body = append(body, fmt.Sprintf("%d unavailable entries are skipped.", unavailable))
	}
	opt := "git fetch --all"
	if m.app.Config.Git.FetchPrune {
		opt += " --prune"
	}
	body = append(body, "", "Command: "+opt, "", "Enter/y to start, Esc/n to cancel.")
	return m.overlay("Confirm fetch", body)
}

func (m *Model) viewUpdatePreview() string {
	now := m.now()
	var rows []string
	for _, c := range m.updateOK {
		r := c.Repository
		rows = append(rows, styleOK.Render(fmt.Sprintf("  update  %-24s behind %d · %s",
			runewidth.Truncate(r.Name, 24, "…"), r.Upstream.Value.Behind, fetchAge(now, &r))))
	}
	for _, c := range m.updateSkip {
		r := c.Repository
		rows = append(rows, styleDim.Render(fmt.Sprintf("  skip    %-24s %s",
			runewidth.Truncate(r.Name, 24, "…"), c.SkipReason)))
	}
	for i := range rows {
		if i == m.overlayIdx {
			rows[i] = styleSelected.Render(fit(rows[i], m.width))
		}
	}
	header := []string{
		fmt.Sprintf("%d repositories will be fast-forwarded, %d skipped.", len(m.updateOK), len(m.updateSkip)),
		"Command: git merge --ff-only --no-autostash @{upstream} (no network access; fetch first with F)",
	}
	if m.fetchHidden > 0 {
		header = append(header, styleWarn.Render(fmt.Sprintf("%d marked repositories are hidden by the current filter and are included.", m.fetchHidden)))
	}
	header = append(header, "")
	body := append(header, m.window(rows, m.overlayIdx, len(header))...)
	return m.overlay("Safe update preview", body)
}

func (m *Model) viewLaunchers() string {
	var body []string
	for i, it := range m.launcherItems() {
		line := "  " + it.label
		if i == m.overlayIdx {
			line = styleSelected.Render(fit(line, m.width))
		}
		body = append(body, line)
	}
	title := "Launch"
	if r := m.current(); r != nil {
		title += " — " + r.Name
	}
	return m.overlay(title, body)
}
