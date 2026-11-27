package tui

import (
	"fmt"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/mmo-con/headquarters/internal/app"
	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/jobs"
)

// Widths of the age and author columns of commit lines; "59 min" is the
// longest age.
const (
	commitAgeWidth    = 6
	commitAuthorWidth = 12
)

// incomingCount returns the configured number of listed incoming commits.
func (m *Model) incomingCount() int { return m.app.Config.UI.IncomingCommits }

// incomingTarget returns the repository and range whose incoming commits are
// shown: the selected row of the update confirmation with the state it
// confirms, the selected row of the operations view, or the selected
// repository. It reports false if the upstream state is unknown or nothing
// is selected.
func (m *Model) incomingTarget() (domain.RepositoryID, app.IncomingRange, bool) {
	switch m.mode {
	case modeUpdatePreview:
		if c := m.previewSelected(); c != nil {
			rng, ok := app.IncomingOf(&c.Repository)
			return c.Repository.ID, rng, ok
		}
		return "", app.IncomingRange{}, false
	case modeOperations:
		return m.opIncoming()
	}
	if m.currentRow().count > 0 {
		return "", app.IncomingRange{}, false
	}
	r := m.current()
	if r == nil {
		return "", app.IncomingRange{}, false
	}
	rng, ok := app.IncomingOf(r)
	return r.ID, rng, ok
}

// previewSelected returns the selected row of the update confirmation.
func (m *Model) previewSelected() *app.UpdateCandidate {
	p := m.preview
	switch i := m.overlayIdx; {
	case p == nil || i < 0:
		return nil
	case i < len(p.ok):
		return &p.ok[i]
	case i < len(p.ok)+len(p.skip):
		return &p.skip[i-len(p.ok)]
	}
	return nil
}

// opIncoming returns the range of the selected batch row: the planned range
// of an update row, which stays fixed when the fast-forward moves HEAD, or
// the current range of the repository of a fetch row.
func (m *Model) opIncoming() (domain.RepositoryID, app.IncomingRange, bool) {
	b := m.ops.Batch()
	if b == nil || m.opSelected < 0 || m.opSelected >= len(b.Rows) {
		return "", app.IncomingRange{}, false
	}
	row := b.Rows[m.opSelected]
	if b.Op == domain.OperationUpdate {
		return row.Job.RepositoryID, row.Incoming, true
	}
	r := m.repo(row.Job.RepositoryID)
	if r == nil {
		return "", app.IncomingRange{}, false
	}
	rng, ok := app.IncomingOf(r)
	return r.ID, rng, ok
}

// requestIncoming reads the incoming commits of the incoming target in the
// background unless they are cached. Like the recent commits, a request for
// another target is cancelled, so moving through a list starts at most one
// Git process at a time; it reads local objects only and never fetches.
func (m *Model) requestIncoming() {
	n := m.incomingCount()
	id, rng, ok := m.incomingTarget()
	r := m.repo(id)
	if n < 1 || !ok || !rng.Listable() || r == nil || !r.IsAvailable() {
		return
	}
	key := incomingKey{repo: id, rng: rng}
	if e, ok := m.incoming[key]; ok && e.n == n {
		return
	}
	if p := m.incomingPending; p != nil {
		if p.key == key && p.n == n {
			return
		}
		m.forget(p.batch)
	}
	job := app.IncomingJob(m.app.Git, *r, rng, n, m.app.Config.Git.InspectionTimeout)
	m.incomingPending = &incomingRequest{batch: m.submit([]jobs.Job{job}, m.applyIncoming), key: key, n: n}
}

// applyIncoming stores the result of the pending incoming request.
func (m *Model) applyIncoming(ev jobs.Event, _ map[domain.RepositoryID]bool) {
	p := m.incomingPending
	if p == nil || !ev.Result.Status.Final() {
		return
	}
	m.incomingPending = nil
	delete(m.routes, ev.BatchID)
	payload, ok := ev.Payload.(app.IncomingPayload)
	if !ok || payload.Range != p.key.rng {
		return
	}
	m.incoming[p.key] = incomingEntry{n: p.n, commits: payload.Commits, err: ev.Result.Error}
}

// dropIncoming forgets the pending request and the cached incoming commits.
func (m *Model) dropIncoming() {
	if p := m.incomingPending; p != nil {
		m.forget(p.batch)
		m.incomingPending = nil
	}
	m.incoming = map[incomingKey]incomingEntry{}
}

// cachedIncoming returns the cached incoming commits of a range for the
// configured count.
func (m *Model) cachedIncoming(id domain.RepositoryID, rng app.IncomingRange) (incomingEntry, bool) {
	e, ok := m.incoming[incomingKey{repo: id, rng: rng}]
	if !ok || e.n != m.incomingCount() {
		return incomingEntry{}, false
	}
	return e, true
}

// commitLine renders a recent or incoming commit: hash, age, author, and
// subject. Ages are right-aligned, so the authors line up.
func (m *Model) commitLine(now time.Time, c domain.CommitSummary) string {
	t := m.theme
	age := runewidth.FillLeft(commitAge(now, c.Time), commitAgeWidth)
	author := runewidth.FillRight(runewidth.Truncate(domain.Printable(c.Author), commitAuthorWidth, "…"), commitAuthorWidth)
	return line(seg{c.ShortID + "  " + age + "  " + author + "  ", t.dim}, seg{c.Subject, t.plain})
}

// incomingMore renders the line that stands for n commits that are not
// listed.
func (m *Model) incomingMore(n int) string {
	return line(seg{fmt.Sprintf("… and %d more", n), m.theme.dim})
}

// incomingStatus returns a line that replaces the list of a range with
// incoming commits while it cannot be shown, or "" if the commits are cached.
func (m *Model) incomingStatus(id domain.RepositoryID, rng app.IncomingRange) (string, []domain.CommitSummary) {
	dim := m.theme.dim
	if !rng.Listable() {
		return line(seg{fmt.Sprintf("%d commits; the upstream commit is unknown, press r", rng.Behind), dim}), nil
	}
	e, ok := m.cachedIncoming(id, rng)
	switch {
	case !ok:
		return line(seg{"loading…", dim}), nil
	case e.err != nil:
		return line(seg{"unavailable: " + firstLine(e.err.Error()), dim}), nil
	}
	return "", e.commits
}

// incomingLines lists the incoming commits of a range in at most limit lines
// for an overlay, ending with the number of commits not listed.
func (m *Model) incomingLines(now time.Time, id domain.RepositoryID, rng app.IncomingRange, limit int) []string {
	if limit < 1 {
		return nil
	}
	status, commits := m.incomingStatus(id, rng)
	if status != "" {
		return []string{status}
	}
	shown := len(commits)
	if rng.Behind > shown && shown >= limit || shown > limit {
		shown = limit - 1
	}
	var out []string
	for _, c := range commits[:shown] {
		out = append(out, m.commitLine(now, c))
	}
	if more := rng.Behind - shown; more > 0 {
		out = append(out, m.incomingMore(more))
	}
	return out
}
