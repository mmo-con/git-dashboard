package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/mmo-con/headquarters/internal/domain"
)

func (m *Model) detailsBox(w, h int) []string {
	if row := m.currentRow(); row.count > 0 {
		action := "expand"
		if m.isExpanded(row.family) {
			action = "collapse"
		}
		lines := []string{fmt.Sprintf(" Matching repositories: %d", row.count)}
		if summary := m.familySummary(m.familyHealth()[row.family]); len(summary) > 0 {
			lines = append(lines, " "+line(summary...))
		}
		lines = append(lines, "", " Enter: "+action+" family", " Space: mark family", " f / u: fetch / update family")
		return m.box(string(row.family), "", lines, w, h, false)
	}
	r := m.current()
	if r == nil {
		return m.box("Details", "", nil, w, h, false)
	}
	lines := m.detailLines(r, w-4).fit(h - 2)
	for i := range lines {
		lines[i] = " " + lines[i]
	}
	return m.box(r.Name, "", lines, w, h, false)
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
	// detailOlderCommit is a recent commit other than the latest one; these
	// lines are dropped first, oldest first.
	detailOlderCommit
)

// details collects the lines of the detail panel, grouped into sections.
type details struct {
	lines []string
	kinds []detailKind
	// more renders the line that replaces n hidden other branches.
	more func(n int) string
	// width is the width of a section rule, drawn in the rule style.
	width int
	rule  lipgloss.Style
}

func (d *details) add(l string, k detailKind) {
	d.lines = append(d.lines, l)
	d.kinds = append(d.kinds, k)
}

func (d *details) text(ss ...seg) { d.add(line(ss...), detailText) }

// section starts a titled section: the title in its style within a rule
// across the panel, separated from the previous section by a blank line.
func (d *details) section(title string, style lipgloss.Style) {
	if len(d.lines) > 0 {
		d.add("", detailBlank)
	}
	title = strings.ToUpper(title)
	rest := max(d.width-runewidth.StringWidth(title)-4, 0)
	d.add(d.rule.Render("── ")+style.Render(title)+d.rule.Render(" "+strings.Repeat("─", rest)), detailSection)
}

// fit returns at most n lines if possible. At most maxBranchLines other
// branches are listed. If the panel is too small, older commits are dropped
// first, oldest first, then blank lines, then the other branches list is
// shortened, then the section titles are dropped.
func (d *details) fit(n int) []string {
	total, commits := 0, 0
	for _, k := range d.kinds {
		switch k {
		case detailBranch:
			total++
		case detailOlderCommit:
			commits++
		}
	}
	shown := min(total, maxBranchLines)
	for c := commits; c >= 0; c-- {
		if out := d.render(c, shown, total); len(out) <= n {
			return out
		}
	}
	for _, drop := range [][]detailKind{{detailBlank}, {detailBlank, detailSection}} {
		for b := shown; b >= 0; b-- {
			if out := d.render(0, b, total, drop...); len(out) <= n {
				return out
			}
		}
	}
	return d.render(0, 0, total, detailBlank, detailSection)
}

// render returns the lines with at most commits older commits and branches
// other branches, of total, without the kinds in drop.
func (d *details) render(commits, branches, total int, drop ...detailKind) []string {
	var out []string
	seen, older := 0, 0
	for i, l := range d.lines {
		k := d.kinds[i]
		if k == detailOlderCommit {
			older++
			if older <= commits {
				out = append(out, l)
			}
			continue
		}
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

// detailLabelWidth aligns the labels of the detail panel.
const detailLabelWidth = 10

func (m *Model) label(s string) seg {
	return seg{runewidth.FillRight(s, detailLabelWidth), m.theme.dim}
}

// detailLines builds the detail panel for a content width of width cells.
// The status of the branch and the worktree and errors come first, so they
// stay visible when the panel is cut off at the bottom; the static location
// of the repository comes last.
func (m *Model) detailLines(r *domain.Repository, width int) *details {
	t := m.theme
	now := m.now()
	d := &details{more: func(n int) string { return line(seg{fmt.Sprintf("  +%d more", n), t.dim}) },
		width: width, rule: t.inactiveBorder}

	inspected := r.IsAvailable() && loaded(r)
	if inspected {
		d.section("status", t.title)
		b := append([]seg{m.label("branch")}, m.branchSegs(r, true)...)
		if r.Upstream.Valid && r.Upstream.Value.Configured {
			b = append(b, seg{" → " + r.Upstream.Value.Ref, t.dim})
		}
		b = append(b, seg{"  ", t.plain})
		d.text(append(b, m.upstreamSegs(r)...)...)
		d.text(m.label("fetched"), m.fetchAge(now, r))

		if r.Worktree.Valid {
			if r.Operation.Valid && r.Operation.Value != domain.NoOperation {
				d.text(m.label("operation"), seg{r.Operation.Value.String() + " in progress", t.err.Bold(true)})
			}
			d.text(append([]seg{m.label("worktree")}, m.worktreeSegs(r.Worktree.Value)...)...)
			// An unknown count is shown as "?" in the table; a failed
			// inspection has its own section.
			if r.Stashes.Valid && r.Stashes.Value > 0 {
				s := []seg{m.label("stash"), {fmt.Sprintf("%s%d entries", m.icons.stash, r.Stashes.Value), t.warn}}
				if len(m.worktreeSiblings(r)) > 0 {
					s = append(s, seg{"  shared by all worktrees", t.dim})
				}
				d.text(s...)
			}
		}
	}

	if e := m.firstError(r); e != "" {
		d.section("error", t.err.Bold(true))
		d.text(seg{m.icons.failed + " " + firstLine(e), t.err})
		d.text(seg{"press e for details", t.dim})
	}

	if inspected {
		if bs := m.otherBranchLines(r); len(bs) > 0 {
			d.section("other branches", t.title)
			for _, l := range bs {
				d.add(l, detailBranch)
			}
		}

		if r.LastCommit.Valid {
			n := m.historyCount()
			title := "last commit"
			if n > 1 {
				title = "recent commits"
			}
			d.section(title, t.title)
			commitLine := func(c domain.CommitSummary) string {
				return line(seg{c.ShortID + "  " + commitTime(now, c.Time) + "  ", t.dim}, seg{c.Subject, t.plain})
			}
			if c := r.LastCommit.Value; c == nil {
				d.text(seg{"no commits yet", t.dim})
			} else if e, ok := m.recentCommits(r); ok && e.err == nil && len(e.commits) > 0 {
				d.add(commitLine(e.commits[0]), detailText)
				for _, older := range e.commits[1:] {
					d.add(commitLine(older), detailOlderCommit)
				}
			} else {
				d.add(commitLine(*c), detailText)
				if ok && e.err != nil {
					d.add(line(seg{"older commits unavailable: " + firstLine(e.err.Error()), t.dim}), detailOlderCommit)
				}
			}
		}

		if r.Remotes.Valid {
			d.section("remotes", t.title)
			// Remote names take the label column, unless one is longer.
			nameWidth := detailLabelWidth - 2
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
			d.section("linked worktrees", t.title)
			for _, p := range siblings {
				d.text(seg{shortenHome(p), t.dim})
			}
		}
	}

	d.section("repository", t.title)
	d.text(m.label("path"), seg{shortenHome(r.Path), t.dim})
	d.text(m.label("family"), seg{string(r.Family), t.key})
	if r.Workspace != "" && r.Workspace != r.Path {
		d.text(m.label("workspace"), seg{shortenHome(r.Workspace), t.dim})
	}
	d.text(m.label("source"), seg{sourceLabel(r), t.dim})
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
	case r.Inspected.Err != nil:
		return r.Inspected.Err.Error()
	}
	if last := m.lastOp(r.ID); last != nil && last.Error != nil {
		return fmt.Sprintf("%s %s: %v", last.Operation, last.Status, last.Error)
	}
	return ""
}
