package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/mmo-con/headquarters/internal/app"
	"github.com/mmo-con/headquarters/internal/config"
	"github.com/mmo-con/headquarters/internal/domain"
)

// modal returns the dialog for the current mode, or nil.
func (m *Model) modal(h int) []string {
	type spec struct {
		width int
		build func(iw, rows int) (string, []string)
	}
	specs := map[mode]spec{
		modeHelp:           {96, m.viewHelp},
		modeError:          {100, m.viewError},
		modeSort:           {40, m.viewSort},
		modeFamily:         {50, m.viewFamily},
		modeOperations:     {100, m.viewOperations},
		modeUpdatePreview:  {100, m.viewUpdatePreview},
		modeCleanupPreview: {100, m.viewCleanupPreview},
		modeLaunchers:      {50, m.viewLaunchers},
		modeCopy:           {70, m.viewCopy},
		modeConfirmQuit:    {72, m.viewConfirmQuit},
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
	legend := []BindingGroup{
		{Title: "Health column (most important state only)", Bindings: m.icons.healthLegend()},
		{Title: "Symbols", Bindings: m.icons.legend()},
	}
	var left, right []string
	if iw >= 84 {
		var lg, rg []BindingGroup
		for _, g := range Bindings {
			if g.Right {
				rg = append(rg, g)
			} else {
				lg = append(lg, g)
			}
		}
		left = col(append(lg, legend[0]))
		right = col(append(rg, legend[1]))
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
	return "Help", m.scroll(col(append(append([]BindingGroup{}, Bindings...), legend...)), rows)
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
		if r.Inspected.Err != nil {
			body = append(body, renderLines(t.err, "", "inspection: "+r.Inspected.Err.Error())...)
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
		mark := " "
		if k == m.sortKey {
			mark = m.icons.clean
		}
		// The first letter of the configuration name selects the order.
		text := mark + " " + config.SortOrders[i][:1] + "  " + k.String()
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
	b := m.ops.Batch()
	if b == nil {
		return nil
	}
	var order []int
	for _, g := range opGroups {
		for i, row := range b.Rows {
			if row.Result.Status == g.status {
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
	b := m.ops.Batch()
	if b == nil {
		return "Operations", []string{"no operations"}
	}
	t := m.theme
	counts := map[domain.OperationStatus]int{}
	for _, r := range b.Rows {
		counts[r.Result.Status]++
	}
	total := len(b.Rows)
	done := counts[domain.StatusSucceeded] + counts[domain.StatusFailed] + counts[domain.StatusSkipped] + counts[domain.StatusCancelled]
	summary := fmt.Sprintf(" %3d%% · %d/%d · %d ok · %d failed · %d skipped", done*100/max(total, 1), done, total,
		counts[domain.StatusSucceeded], counts[domain.StatusFailed], counts[domain.StatusSkipped])
	bar := m.progressBar(done, total, max(iw-ansi.StringWidth(summary), 10))
	head := []string{bar + summary}
	op := string(b.Op)
	if m.ops.FetchForUpdate() {
		op = "update: fetch"
		head = append(head, t.dim.Render("Fetching first; the update starts when the fetch is finished."))
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
		for i, row := range b.Rows {
			if row.Result.Status != g.status {
				continue
			}
			group = append(group, title)
			name := string(row.Job.RepositoryID)
			if r := m.repo(row.Job.RepositoryID); r != nil {
				name = r.Name
			}
			text := fmt.Sprintf(" %s %s", icon, runewidth.FillRight(runewidth.Truncate(domain.Printable(name), 24, "…"), 24))
			if b.Op == domain.OperationCleanup {
				text += " " + runewidth.FillRight(runewidth.Truncate(domain.Printable(row.Branch), 24, "…"), 24)
			}
			if row.Result.Status.Final() && row.Result.Error != nil {
				text += " " + firstLine(row.Result.Error.Error())
			}
			selected := i == m.opSelected
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
	if m.opSelected < total {
		res := b.Rows[m.opSelected].Result
		if s := strings.TrimSpace(res.Stderr); s != "" && res.Status == domain.StatusFailed {
			detail = append(detail, "", t.panelTitle.Render("stderr"))
			detail = append(detail, renderLines(t.dim, "  ", s)...)
			if len(detail) > 8 {
				detail = append(detail[:8], t.dim.Render("  …"))
			}
		}
	}
	state := "running"
	if !m.ops.Running() {
		state = "finished"
	}
	var incoming []string
	id, rng, known := m.opIncoming()
	if known && rng.Behind > 0 && m.incomingCount() > 0 {
		title := "Incoming commits as of the last fetch"
		if b.Op == domain.OperationUpdate {
			title = "Commits of this update"
		}
		incoming = []string{"", t.panelTitle.Render(title)}
	}
	avail := rows - len(head) - len(detail)
	if len(incoming) > 0 {
		// The list keeps a few rows; the incoming commits take the rest up to
		// the configured count and the line of omitted commits.
		want := len(incoming) + min(m.incomingCount(), rng.Behind) + 1
		listRows := min(len(list), max(avail-want, min(len(list), 4)))
		if lines := m.incomingLines(m.now(), id, rng, avail-listRows-len(incoming)); len(lines) > 0 {
			incoming = append(incoming, lines...)
			avail = listRows
		} else {
			incoming = nil
		}
	}
	body := append(head, operationWindow(list, group, selLine, avail)...)
	body = append(body, incoming...)
	return fmt.Sprintf("%s ─ %d repositories ─ %s", op, total, state), append(body, detail...)
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

func (m *Model) viewConfirmQuit(iw, rows int) (string, []string) {
	t := m.theme
	b, open := m.ops.Batch(), 0
	for _, r := range b.Rows {
		if !r.Result.Status.Final() {
			open++
		}
	}
	head := fmt.Sprintf("The %s batch has %d unfinished repositories.", b.Op, open)
	if open == 0 && m.ops.UpdatePending() {
		head = "The update has not started yet; its fetch is being checked."
	}
	return "Quit", []string{
		t.bold.Render(head),
		"",
		"Quitting cancels them. Running Git processes are asked to stop,",
		"so that they can remove their lock files.",
		"",
		m.hints("Enter/y", "quit", "Esc/n", "keep running"),
	}
}

// viewUpdatePreview confirms an update. The list shows the state before the
// fetch; which repositories are fast-forwarded is decided after it.
func (m *Model) viewUpdatePreview(iw, rows int) (string, []string) {
	t, p := m.theme, m.preview
	now := m.now()
	var list []string
	idx := 0
	for _, c := range p.ok {
		r := c.Repository
		text := fmt.Sprintf("%s behind   %-24s %s%d · %s", m.icons.behind, ansi.Truncate(r.Name, 24, "…"),
			m.icons.behind, r.Upstream.Value.Behind, m.fetchSeg(now, &r).text)
		list = append(list, m.selectable(text, idx == m.overlayIdx, iw, t.behind))
		idx++
	}
	for _, c := range p.skip {
		r := c.Repository
		reason := c.Verdict.String()
		if count := m.incomingCountText(&r); count != "" {
			reason = count + " · " + reason
		}
		text := fmt.Sprintf("– blocked  %-24s %s", ansi.Truncate(r.Name, 24, "…"), reason)
		style := t.warn
		if c.Verdict.UpToDate() {
			text = fmt.Sprintf("%s current  %-24s %s", m.icons.upToDate, ansi.Truncate(r.Name, 24, "…"), m.fetchSeg(now, &r).text)
			style = t.dim
		}
		list = append(list, m.selectable(text, idx == m.overlayIdx, iw, style))
		idx++
	}
	n := len(p.ok) + len(p.skip)
	head := []string{
		t.bold.Render(fmt.Sprintf("Fetch and update %d repositories?", n)),
		t.dim.Render("Fetches first, then fast-forwards branches that can be updated safely."),
		t.dim.Render("No merge commits, rebases, or stashes; local changes stay untouched."),
		t.dim.Render("Repositories that cannot be updated are listed with the reason."),
	}
	if p.hidden > 0 {
		head = append(head, t.warn.Render(m.hiddenMarksNotice(p.hidden)))
	}
	head = append(head, "", t.panelTitle.Render("State before the fetch"))
	// While any row has incoming commits, the list leaves room for them, so
	// its height does not change while the selection moves.
	reserve := 0
	if m.incomingCount() > 0 {
		for _, c := range append(append([]app.UpdateCandidate{}, p.ok...), p.skip...) {
			if rng, ok := app.IncomingOf(&c.Repository); ok && rng.Behind > 0 {
				reserve = 6
				break
			}
		}
	}
	body := append(head, window(list, m.overlayIdx, min(len(list), max(rows-len(head)-reserve, 3)))...)
	if c := m.previewSelected(); c != nil && reserve > 0 {
		r := c.Repository
		if rng, ok := app.IncomingOf(&r); ok && rng.Behind > 0 {
			title := []string{"", t.panelTitle.Render("Incoming to " + r.Name + " as of the last fetch")}
			if lines := m.incomingLines(now, r.ID, rng, rows-len(body)-len(title)); len(lines) > 0 {
				body = append(append(body, title...), lines...)
			}
		}
	}
	return "Confirm update", body
}

// incomingCountText is the number of incoming commits of r for the update
// confirmation: "" without incoming commits, "?" if the upstream state is
// unknown.
func (m *Model) incomingCountText(r *domain.Repository) string {
	rng, ok := app.IncomingOf(r)
	switch {
	case !ok:
		return m.icons.behind + "?"
	case rng.Behind > 0:
		return fmt.Sprintf("%s%d", m.icons.behind, rng.Behind)
	}
	return ""
}

func (m *Model) hiddenMarksNotice(hidden int) string {
	if m.tree {
		return fmt.Sprintf("Includes %d marked repos hidden by filters or collapsed groups.", hidden)
	}
	return fmt.Sprintf("Includes %d marked repositories hidden by the filter.", hidden)
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
