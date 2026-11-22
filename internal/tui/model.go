// Package tui implements the Bubble Tea user interface. It consumes the
// application services and never constructs Git commands itself.
package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mmo-con/git-dashboard/internal/app"
	"github.com/mmo-con/git-dashboard/internal/domain"
	"github.com/mmo-con/git-dashboard/internal/jobs"
)

type mode int

const (
	modeNormal mode = iota
	modeFilter
	modeFamily
	modeHelp
	modeError
	modeSort
	modeOperations
	modeUpdatePreview
	modeLaunchers
	modeConfirmQuit
)

type repoRow struct {
	id     domain.RepositoryID
	family domain.FamilyID
	count  int // nonzero for a family header
}

// opRow is one repository of a batch. Repositories that the update preview
// skipped are kept as rows that never run, so that the batch lists every
// repository that still needs attention.
type opRow struct {
	job    jobs.Job
	result domain.OperationResult
	// revisitedAt is the time the user returned from lazygit after opening
	// the repository from the operations view.
	revisitedAt time.Time
}

// refetch remembers the targets of a confirmed update while they are
// fetched, and the inspections that follow the fetch. The update is planned
// only after these inspections, so it applies the fetched upstream state.
type refetch struct {
	batch       *batch
	targets     []domain.RepositoryID
	inspections []uint64
}

// historyEntry holds the recent commits of a repository for the detail
// panel. It stays valid while HEAD points to head and the configured count
// is n.
type historyEntry struct {
	head    string
	n       int
	commits []domain.CommitSummary
	err     error
}

// historyRequest is the history job for the selected repository.
type historyRequest struct {
	batch uint64
	repo  domain.RepositoryID
	head  string
	n     int
}

// visit remembers which batch row was opened in lazygit.
type visit struct {
	batch *batch
	row   int
}

type batch struct {
	id   uint64
	op   domain.OperationType
	rows []opRow
}

func (b *batch) done() bool {
	for _, r := range b.rows {
		if !r.result.Status.Final() {
			return false
		}
	}
	return true
}

// Model is the Bubble Tea model of Git Dashboard.
type Model struct {
	app *app.Application

	repos   []domain.Repository
	index   map[domain.RepositoryID]int
	visible []domain.RepositoryID // shown repositories, independent of the viewport
	rows    []repoRow
	cursor  int
	repoTop int
	marks   map[domain.RepositoryID]bool

	tree     bool
	expanded map[domain.FamilyID]bool

	filter      app.Filter
	families    []domain.FamilyID
	familyIdx   int
	familyQuery string
	sortKey     app.SortKey

	mode       mode
	overlayIdx int
	width      int
	height     int

	loadingInventory bool
	// inspecting counts the unfinished jobs of each inspection batch of the
	// current inventory. Events of other batches belong to an earlier
	// inventory and are ignored.
	inspecting map[uint64]int
	warnings   []string
	message    string
	messageErr bool

	batch   *batch
	lastOps map[domain.RepositoryID]domain.OperationResult

	updateHidden  int
	updateOK      []app.UpdateCandidate
	updateSkip    []app.UpdateCandidate
	updateTargets []domain.RepositoryID
	previewReturn mode
	// refetch is the fetch of a confirmed update; the update starts on its
	// own once the fetch and the following inspections are finished.
	refetch    *refetch
	quitReturn mode
	visit      *visit

	// history caches recent commits for the detail panel; only the selected
	// repository is ever requested, one at a time.
	history        map[domain.RepositoryID]historyEntry
	historyPending *historyRequest

	handovers map[domain.StoreID]bool
	fatal     error
	now       func() time.Time

	theme    theme
	icons    icons
	frame    int
	spinning bool
	msgSeq   uint64
	msgTimer uint64
}

// New creates the model.
func New(a *app.Application) *Model {
	return &Model{
		app:              a,
		index:            map[domain.RepositoryID]int{},
		marks:            map[domain.RepositoryID]bool{},
		expanded:         map[domain.FamilyID]bool{},
		lastOps:          map[domain.RepositoryID]domain.OperationResult{},
		handovers:        map[domain.StoreID]bool{},
		history:          map[domain.RepositoryID]historyEntry{},
		inspecting:       map[uint64]int{},
		loadingInventory: true,
		width:            100,
		height:           30,
		now:              time.Now,
		theme:            newTheme(a.Config.UI.Theme),
		icons:            iconSet(a.Config.UI.Icons),
		tree:             a.Config.UI.View == "tree",
	}
}

// Err returns a fatal error that ended the program, if any.
func (m *Model) Err() error { return m.fatal }

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadInventory(), m.waitEvents(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// loadInventory builds the inventory of the active configuration. The
// configuration and the client are captured here, in the update loop, so the
// command does not access the Application concurrently.
func (m *Model) loadInventory() tea.Cmd {
	cfg, client := m.app.Config, m.app.Git
	return func() tea.Msg {
		return inventoryMsg{result: app.BuildInventory(context.Background(), cfg, client)}
	}
}

// reload loads the configuration and builds its inventory in the background.
// The update loop activates both together when inventoryMsg arrives.
func (m *Model) reload() tea.Cmd {
	a := m.app
	return func() tea.Msg {
		l, err := a.LoadConfig(context.Background())
		if err != nil {
			return reloadFailedMsg{err: err}
		}
		return inventoryMsg{loaded: l, result: app.BuildInventory(context.Background(), l.Config, l.Git)}
	}
}

// waitEvents is the single forwarder from the scheduler to Bubble Tea. Job
// workers push to an unbounded queue and never block on the user interface.
func (m *Model) waitEvents() tea.Cmd {
	q := m.app.Scheduler.Events
	return func() tea.Msg {
		ev := q.Next()
		if ev == nil {
			return nil
		}
		return eventsMsg{events: ev}
	}
}

func (m *Model) repo(id domain.RepositoryID) *domain.Repository {
	if i, ok := m.index[id]; ok {
		return &m.repos[i]
	}
	return nil
}

func (m *Model) currentRow() repoRow {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return repoRow{}
	}
	return m.rows[m.cursor]
}

func (m *Model) current() *domain.Repository {
	return m.repo(m.currentRow().id)
}

func (m *Model) lastOp(id domain.RepositoryID) *domain.OperationResult {
	if r, ok := m.lastOps[id]; ok {
		return &r
	}
	return nil
}

// setMessage and setError show a toast. External text in msg is made
// printable.
func (m *Model) setMessage(msg string) {
	m.message, m.messageErr = domain.Printable(msg), false
	m.msgSeq++
}

func (m *Model) setError(msg string) {
	m.message, m.messageErr = domain.Printable(msg), true
	m.msgSeq++
}

// busy reports whether a spinner should be animated.
func (m *Model) busy() bool {
	return m.loadingInventory || m.pendingInspect() > 0 || m.batchRunning()
}

// pendingInspect returns the number of unfinished inspections.
func (m *Model) pendingInspect() int {
	n := 0
	for _, c := range m.inspecting {
		n += c
	}
	return n
}

// batchRunning reports whether the current fetch or update batch has
// unfinished jobs, or a confirmed update still waits for its fetch.
func (m *Model) batchRunning() bool {
	return (m.batch != nil && !m.batch.done()) || m.refetch != nil
}

// recompute applies filter and sorting while keeping the cursor on the same
// repository where possible.
func (m *Model) recompute() {
	keep := m.currentRow()
	m.filter.Family = ""
	if m.familyIdx > 0 && m.familyIdx <= len(m.families) {
		m.filter.Family = m.families[m.familyIdx-1]
	}
	var list []*domain.Repository
	for i := range m.repos {
		r := &m.repos[i]
		if m.filter.Matches(r, m.marks[r.ID]) {
			list = append(list, r)
		}
	}
	app.Sort(list, m.sortKey)
	m.rows = m.rows[:0]
	m.visible = m.visible[:0]
	addRepo := func(r *domain.Repository) {
		m.rows = append(m.rows, repoRow{id: r.ID, family: r.Family})
		m.visible = append(m.visible, r.ID)
	}
	if m.tree {
		groups := make(map[domain.FamilyID][]*domain.Repository)
		for _, r := range list {
			groups[r.Family] = append(groups[r.Family], r)
		}
		for _, family := range m.families {
			group := groups[family]
			if len(group) == 0 {
				continue
			}
			m.rows = append(m.rows, repoRow{family: family, count: len(group)})
			if m.expanded[family] {
				for _, r := range group {
					addRepo(r)
				}
			}
		}
	} else {
		for _, r := range list {
			addRepo(r)
		}
	}

	to := m.cursor
	if keep.id != "" || keep.count > 0 {
		fallback := -1
		for i, row := range m.rows {
			if keep.id != "" && row.id == keep.id {
				fallback = i
				break
			}
			if fallback == -1 && row.family == keep.family && (row.count > 0 || (keep.count > 0 && !m.tree)) {
				fallback = i
			}
		}
		if fallback >= 0 {
			to = fallback
		}
	}
	m.moveRepoCursor(to)
}

func (m *Model) moveRepoCursor(to int) {
	m.cursor = min(max(to, 0), max(len(m.rows)-1, 0))
	m.adjustRepoViewport()
}

func (m *Model) adjustRepoViewport() {
	rows := m.tableRows()
	margin := min(2, (rows-1)/2)
	if m.cursor < m.repoTop+margin {
		m.repoTop = m.cursor - margin
	} else if m.cursor >= m.repoTop+rows-margin {
		m.repoTop = m.cursor - rows + margin + 1
	}
	m.repoTop = min(max(m.repoTop, 0), max(len(m.rows)-rows, 0))
}

func (m *Model) rebuildFamilies() {
	var current domain.FamilyID
	if m.familyIdx > 0 && m.familyIdx <= len(m.families) {
		current = m.families[m.familyIdx-1]
	}
	seen := map[domain.FamilyID]bool{}
	m.families = m.families[:0]
	for _, r := range m.repos {
		if !seen[r.Family] {
			seen[r.Family] = true
			m.families = append(m.families, r.Family)
		}
	}
	sortFamilies(m.families)
	m.familyIdx = 0
	for i, f := range m.families {
		if f == current {
			m.familyIdx = i + 1
		}
	}
	if m.mode == modeFamily {
		m.overlayIdx = min(m.overlayIdx, max(len(m.familyChoices())-1, 0))
	}
}

func sortFamilies(f []domain.FamilyID) {
	for i := 1; i < len(f); i++ {
		for j := i; j > 0 && f[j] < f[j-1]; j-- {
			f[j], f[j-1] = f[j-1], f[j]
		}
	}
}

func (m *Model) familyLabel(i int) string {
	if i == 0 {
		return "All families"
	}
	return string(m.families[i-1])
}

func (m *Model) familyChoices() []int {
	query := strings.ToLower(strings.TrimSpace(m.familyQuery))
	var choices []int
	for i := 0; i <= len(m.families); i++ {
		if strings.Contains(strings.ToLower(m.familyLabel(i)), query) {
			choices = append(choices, i)
		}
	}
	return choices
}

// refresh starts inspection of the given entries and returns the ID of the
// inspection batch, or 0 if nothing is inspected.
func (m *Model) refresh(ids []domain.RepositoryID) uint64 {
	var repos []domain.Repository
	seen := map[domain.RepositoryID]bool{}
	for _, id := range ids {
		if e, ok := m.history[id]; ok && e.err != nil {
			delete(m.history, id)
		}
		if r := m.repo(id); r != nil && !seen[id] {
			seen[id] = true
			repos = append(repos, *r)
		}
	}
	gen := m.app.NextGeneration()
	js := app.InspectJobs(m.app.Git, repos, gen, m.app.Config.Git.InspectionTimeout)
	if len(js) == 0 {
		return 0
	}
	id := m.app.Scheduler.Submit(js)
	m.inspecting[id] = len(js)
	return id
}

func (m *Model) allIDs() []domain.RepositoryID {
	ids := make([]domain.RepositoryID, len(m.repos))
	for i, r := range m.repos {
		ids[i] = r.ID
	}
	return ids
}

// targets returns the targets of a batch and the number of marked
// repositories hidden from the list. With visible, these are the visible
// repositories regardless of marks; otherwise the marked repositories if any
// are marked, else the repository under the cursor.
func (m *Model) targets(visible bool) (list []domain.Repository, hidden int) {
	if visible {
		for _, id := range m.visible {
			list = append(list, *m.repo(id))
		}
		return list, 0
	}
	if len(m.marks) > 0 {
		vis := map[domain.RepositoryID]bool{}
		for _, id := range m.visible {
			vis[id] = true
		}
		for i := range m.repos {
			if m.marks[m.repos[i].ID] {
				list = append(list, m.repos[i])
				if !vis[m.repos[i].ID] {
					hidden++
				}
			}
		}
		return list, hidden
	}
	if r := m.current(); r != nil {
		list = append(list, *r)
	}
	return list, 0
}

// historyCount returns the configured number of recent commits.
func (m *Model) historyCount() int { return m.app.Config.UI.DetailCommits }

// requestHistory reads the recent commits of the selected repository in the
// background unless they are cached for its current HEAD. A request for
// another repository is cancelled, so moving through the list starts at
// most one Git process at a time. A count of 1 needs no process, because
// inspection already reads the latest commit.
func (m *Model) requestHistory() {
	n := m.historyCount()
	r := m.current()
	if n <= 1 || r == nil || !r.IsAvailable() || !r.Head.Valid || r.Head.Value.Unborn || r.Head.Value.CommitID == "" {
		return
	}
	head := r.Head.Value.CommitID
	if e, ok := m.history[r.ID]; ok && e.head == head && e.n == n {
		return
	}
	if p := m.historyPending; p != nil {
		if p.repo == r.ID && p.head == head && p.n == n {
			return
		}
		m.app.Scheduler.Cancel(p.batch)
	}
	job := app.HistoryJob(m.app.Git, *r, n, m.app.Config.Git.InspectionTimeout)
	m.historyPending = &historyRequest{batch: m.app.Scheduler.Submit([]jobs.Job{job}), repo: r.ID, head: head, n: n}
}

// applyHistory stores the result of the pending history request. Results of
// cancelled or earlier requests are ignored.
func (m *Model) applyHistory(ev jobs.Event) {
	p := m.historyPending
	if !ev.Result.Status.Final() {
		return
	}
	m.historyPending = nil
	payload, ok := ev.Payload.(app.HistoryPayload)
	if !ok || payload.Head != p.head {
		return
	}
	m.history[p.repo] = historyEntry{head: p.head, n: p.n, commits: payload.Commits, err: payload.Err}
}

// recentCommits returns the cached recent commits of r for its current HEAD.
func (m *Model) recentCommits(r *domain.Repository) (historyEntry, bool) {
	e, ok := m.history[r.ID]
	if !ok || !r.Head.Valid || e.head != r.Head.Value.CommitID || e.n != m.historyCount() {
		return historyEntry{}, false
	}
	return e, true
}

// storeBusy reports whether a mutating job or a handover is active or queued
// for the store.
func (m *Model) storeBusy(id domain.StoreID) string {
	if m.handovers[id] {
		return "an interactive session"
	}
	if m.batch != nil {
		for _, row := range m.batch.rows {
			if row.job.StoreID == id && !row.result.Status.Final() {
				return string(row.job.Operation)
			}
		}
	}
	return ""
}
