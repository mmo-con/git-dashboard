package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/mmo-con/git-dashboard/internal/app"
	"github.com/mmo-con/git-dashboard/internal/domain"
)

func loaded(r *domain.Repository) bool {
	return !r.Inspection.FinishedAt.IsZero()
}

// statusSegs describes the worktree. Every symbol carries a count or text, so
// color is never the only carrier of information.
func (m *Model) statusSegs(r *domain.Repository) []seg {
	t, ic := m.theme, m.icons
	if !r.IsAvailable() {
		return []seg{{ic.unavailable + " " + r.Availability.String(), t.err}}
	}
	if !loaded(r) {
		return []seg{{m.spinnerFrame(), t.dim}}
	}
	if !r.Worktree.Valid {
		return []seg{{"? unknown", t.warn}}
	}
	var out []seg
	add := func(s seg) {
		if len(out) > 0 {
			out = append(out, seg{"  ", t.plain})
		}
		out = append(out, s)
	}
	if r.Operation.Valid && r.Operation.Value != domain.NoOperation {
		add(seg{r.Operation.Value.String(), t.err.Bold(true)})
	}
	w := r.Worktree.Value
	for _, c := range []struct {
		icon string
		n    int
		seg  seg
	}{
		{ic.conflict, w.Conflicts, seg{style: t.err}},
		{ic.staged, w.Staged, seg{style: t.ok}},
		{ic.modified, w.Modified, seg{style: t.dirty}},
		{ic.deleted, w.Deleted, seg{style: t.dirty}},
		{ic.untracked, w.Untracked, seg{style: t.dim}},
	} {
		if c.n > 0 {
			add(seg{fmt.Sprintf("%s%d", c.icon, c.n), c.seg.style})
		}
	}
	if len(out) == 0 {
		add(seg{ic.clean + " clean", t.clean})
	}
	if r.RemoteMismatch() {
		add(seg{ic.mismatch + "remote", t.warn})
	}
	return out
}

// runningOp returns the operation that is queued or running for r's store.
func (m *Model) runningOp(r *domain.Repository) (domain.OperationType, domain.OperationStatus) {
	if m.batch == nil || r.StoreID == "" {
		return "", ""
	}
	for _, row := range m.batch.rows {
		if row.job.StoreID == r.StoreID && !row.result.Status.Final() {
			return row.job.Operation, row.result.Status
		}
	}
	return "", ""
}

func (m *Model) upstreamSegs(r *domain.Repository) []seg {
	t, ic := m.theme, m.icons
	if op, status := m.runningOp(r); op != "" {
		if status == domain.StatusRunning {
			verb := map[domain.OperationType]string{domain.OperationFetch: "fetching", domain.OperationUpdate: "updating"}[op]
			return []seg{{m.spinnerFrame() + " " + verb, t.key}}
		}
		return []seg{{ic.queued + " queued", t.dim}}
	}
	if last := m.lastOp(r.ID); last != nil && last.Operation == domain.OperationFetch && last.Status == domain.StatusFailed {
		return []seg{{ic.failed + " fetch failed", t.err}}
	}
	if !r.IsAvailable() {
		return []seg{{"–", t.dim}}
	}
	if !loaded(r) {
		return []seg{{m.spinnerFrame(), t.dim}}
	}
	if !r.Head.Valid || !r.Upstream.Valid {
		return []seg{{"? unknown", t.warn}}
	}
	h, u := r.Head.Value, r.Upstream.Value
	switch {
	case h.Unborn:
		return []seg{{"no commits", t.dim}}
	case h.Detached:
		return []seg{{ic.detached + " detached", t.warn}}
	case !u.Configured:
		return []seg{{ic.noUpstream + " no upstream", t.dim}}
	case u.Gone:
		return []seg{{ic.noUpstream + " upstream gone", t.warn}}
	case u.Diverged():
		return []seg{{fmt.Sprintf("%s%d %s%d diverged", ic.ahead, u.Ahead, ic.behind, u.Behind), t.err}}
	case u.Behind > 0:
		return []seg{{fmt.Sprintf("%s%d", ic.behind, u.Behind), t.behind}}
	case u.Ahead > 0:
		return []seg{{fmt.Sprintf("%s%d", ic.ahead, u.Ahead), t.ahead}}
	}
	return []seg{{ic.upToDate, t.clean}}
}

// branchesMarker counts the other local branches that need attention. It is
// highlighted if any of them holds unpublished work.
func (m *Model) branchesMarker(r *domain.Repository) []seg {
	n := len(r.Branches.Value)
	if !r.IsAvailable() || n == 0 {
		return nil
	}
	style := m.theme.behind
	if r.UnpublishedBranches() > 0 {
		style = m.theme.warn
	}
	return []seg{{" │ ", m.theme.dim}, {fmt.Sprintf("%s%d", m.icons.branches, n), style}}
}

// health summarizes the most important state of r in a single symbol, in
// the order errors, dirty worktree, behind, ahead, and other attention.
// healthy is true for an available, inspected repository without findings.
func (m *Model) health(r *domain.Repository) (s seg, healthy bool) {
	t, ic := m.theme, m.icons
	if !r.IsAvailable() || m.firstError(r) != "" {
		return seg{ic.failed, t.err}, false
	}
	if !loaded(r) || !r.Worktree.Valid || !r.Head.Valid || !r.Upstream.Valid {
		return seg{"", t.dim}, false
	}
	w, h, u := r.Worktree.Value, r.Head.Value, r.Upstream.Value
	switch {
	case w.Conflicts > 0 || (r.Operation.Valid && r.Operation.Value != domain.NoOperation) || u.Diverged():
		return seg{ic.conflict, t.err}, false
	case !w.Clean():
		return seg{ic.modified, t.dirty}, false
	case u.Behind > 0:
		return seg{ic.behind, t.behind}, false
	case u.Ahead > 0:
		return seg{ic.ahead, t.ahead}, false
	case r.UnpublishedBranches() > 0 || r.RemoteMismatch() || h.Detached || u.Gone:
		return seg{ic.attention, t.warn}, false
	case len(r.Branches.Value) > 0:
		return seg{ic.attention, t.behind}, false
	}
	return seg{ic.clean, t.clean}, true
}

// maxBranchLines limits the other branches listed in the detail panel.
const maxBranchLines = 10

// otherBranchLines lists the other local branches that need attention.
func (m *Model) otherBranchLines(r *domain.Repository) []string {
	t, ic := m.theme, m.icons
	bs := r.Branches.Value
	if len(bs) == 0 {
		return nil
	}
	now := m.now()
	out := []string{m.theme.header.Render("OTHER BRANCHES")}
	for i, b := range bs {
		if i == maxBranchLines {
			out = append(out, line(seg{fmt.Sprintf("  +%d more", len(bs)-i), t.dim}))
			break
		}
		var state seg
		switch {
		case b.Missing:
			state = seg{ic.unavailable + " missing", t.warn}
		case b.Upstream == "":
			state = seg{ic.noUpstream + " no upstream", t.warn}
		case b.Gone:
			state = seg{ic.noUpstream + " upstream gone", t.warn}
		case b.Ahead > 0 && b.Behind > 0:
			state = seg{fmt.Sprintf("%s%d %s%d diverged", ic.ahead, b.Ahead, ic.behind, b.Behind), t.err}
		case b.Ahead > 0:
			state = seg{fmt.Sprintf("%s%d", ic.ahead, b.Ahead), t.ahead}
		default:
			state = seg{fmt.Sprintf("%s%d", ic.behind, b.Behind), t.behind}
		}
		l := []seg{{"  " + ic.branch + b.Name + "  ", t.branch}, state}
		if !b.Missing {
			l = append(l, seg{" · " + ago(now, b.CommitTime), t.dim})
		}
		out = append(out, line(l...))
	}
	return out
}

func (m *Model) branchSegs(r *domain.Repository) []seg {
	if !r.IsAvailable() || !r.Head.Valid {
		return []seg{{"–", m.theme.dim}}
	}
	h := r.Head.Value
	if h.Detached {
		id := "detached"
		if len(h.CommitID) >= 7 {
			id = h.CommitID[:7]
		}
		return []seg{{m.icons.branch + "(" + id + ")", m.theme.warn}}
	}
	return []seg{{m.icons.branch + h.Branch, m.theme.branch}}
}

func (m *Model) statusText(r *domain.Repository) string   { return plain(m.statusSegs(r)) }
func (m *Model) upstreamText(r *domain.Repository) string { return plain(m.upstreamSegs(r)) }

func (m *Model) spinnerFrame() string {
	f := m.icons.spinner
	return f[m.frame%len(f)]
}

func ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d d ago", int(d.Hours()/24))
	}
	return t.Format("2006-01-02")
}

func shortAgo(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return fmt.Sprintf("%dy", int(d.Hours()/24/365))
}

// staleFetch is the age after which the last fetch is highlighted.
const staleFetch = 7 * 24 * time.Hour

// followUp describes the current state of a row of a finished update batch
// that was not updated. A row that became eligible or up to date is always
// reported; a row that is still blocked only after the repository was
// inspected again following a lazygit session opened from the row.
func (m *Model) followUp(row opRow) (text string, resolved bool) {
	if m.batch == nil || m.batch.op != domain.OperationUpdate || !m.batch.done() ||
		row.result.Status == domain.StatusSucceeded {
		return "", false
	}
	r := m.repo(row.job.RepositoryID)
	if r == nil || !r.Inspection.FinishedAt.After(row.result.FinishedAt) {
		return "", false
	}
	switch reason := app.Eligibility(*r, m.storeBusy(r.StoreID) != ""); {
	case reason == "":
		return "now eligible", true
	case reason == app.ReasonUpToDate:
		return "now up to date", true
	case !row.revisitedAt.IsZero() && r.Inspection.FinishedAt.After(row.revisitedAt):
		return "still blocked: " + reason, false
	}
	return "", false
}

// fetchSeg describes the last fetch; old or missing fetches are highlighted.
func (m *Model) fetchSeg(now time.Time, r *domain.Repository) seg {
	switch {
	case !r.LastFetch.Valid:
		return seg{"fetch time unknown", m.theme.dim}
	case r.LastFetch.Value == nil:
		return seg{"never fetched", m.theme.warn}
	case now.Sub(*r.LastFetch.Value) > staleFetch:
		return seg{"fetched " + ago(now, *r.LastFetch.Value), m.theme.warn}
	}
	return seg{"fetched " + ago(now, *r.LastFetch.Value), m.theme.dim}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
