// Package tui implements the Bubble Tea user interface. It consumes the
// application services and never constructs Git commands itself.
package tui

import (
	"context"
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
	modeHelp
	modeError
	modeSort
	modeOperations
	modeConfirmFetch
	modeUpdatePreview
	modeLaunchers
)

type opRow struct {
	job    jobs.Job
	result domain.OperationResult
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
	visible []domain.RepositoryID
	cursor  int
	marks   map[domain.RepositoryID]bool

	filter    app.Filter
	families  []domain.FamilyID
	familyIdx int
	sortKey   app.SortKey

	mode       mode
	overlayIdx int
	width      int
	height     int

	loadingInventory bool
	pendingInspect   int
	warnings         []string
	message          string
	messageErr       bool

	batch   *batch
	lastOps map[domain.RepositoryID]domain.OperationResult

	fetchTargets []domain.Repository
	fetchHidden  int
	updateOK     []app.UpdateCandidate
	updateSkip   []app.UpdateCandidate

	handovers map[domain.StoreID]bool
	fatal     error
	now       func() time.Time
}

// New creates the model.
func New(a *app.Application) *Model {
	return &Model{
		app:              a,
		index:            map[domain.RepositoryID]int{},
		marks:            map[domain.RepositoryID]bool{},
		lastOps:          map[domain.RepositoryID]domain.OperationResult{},
		handovers:        map[domain.StoreID]bool{},
		loadingInventory: true,
		width:            100,
		height:           30,
		now:              time.Now,
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

func (m *Model) loadInventory() tea.Cmd {
	a := m.app
	return func() tea.Msg {
		return inventoryMsg{result: a.LoadInventory(context.Background())}
	}
}

func (m *Model) reload() tea.Cmd {
	a := m.app
	return func() tea.Msg {
		if err := a.Reload(); err != nil {
			return reloadFailedMsg{err: err}
		}
		return inventoryMsg{result: a.LoadInventory(context.Background())}
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

func (m *Model) current() *domain.Repository {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return m.repo(m.visible[m.cursor])
}

func (m *Model) lastOp(id domain.RepositoryID) *domain.OperationResult {
	if r, ok := m.lastOps[id]; ok {
		return &r
	}
	return nil
}

func (m *Model) setMessage(msg string) { m.message, m.messageErr = msg, false }
func (m *Model) setError(msg string)   { m.message, m.messageErr = msg, true }

// recompute applies filter and sorting while keeping the cursor on the same
// repository where possible.
func (m *Model) recompute() {
	var keep domain.RepositoryID
	if r := m.current(); r != nil {
		keep = r.ID
	}
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
	m.visible = m.visible[:0]
	for _, r := range list {
		m.visible = append(m.visible, r.ID)
	}
	m.cursor = min(m.cursor, max(len(m.visible)-1, 0))
	if keep != "" {
		for i, id := range m.visible {
			if id == keep {
				m.cursor = i
				break
			}
		}
	}
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
}

func sortFamilies(f []domain.FamilyID) {
	for i := 1; i < len(f); i++ {
		for j := i; j > 0 && f[j] < f[j-1]; j-- {
			f[j], f[j-1] = f[j-1], f[j]
		}
	}
}

// refresh starts inspection of the given entries.
func (m *Model) refresh(ids []domain.RepositoryID) {
	var repos []domain.Repository
	seen := map[domain.RepositoryID]bool{}
	for _, id := range ids {
		if r := m.repo(id); r != nil && !seen[id] {
			seen[id] = true
			repos = append(repos, *r)
		}
	}
	gen := m.app.NextGeneration()
	js := app.InspectJobs(m.app.Git, repos, gen, m.app.Config.Git.InspectionTimeout)
	if len(js) == 0 {
		return
	}
	m.pendingInspect += len(js)
	m.app.Scheduler.Submit(js)
}

func (m *Model) allIDs() []domain.RepositoryID {
	ids := make([]domain.RepositoryID, len(m.repos))
	for i, r := range m.repos {
		ids[i] = r.ID
	}
	return ids
}

// targets returns the marked repositories if any are marked, otherwise the
// visible ones, and the number of marked repositories hidden by the filter.
func (m *Model) targets() (list []domain.Repository, hidden int) {
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
	for _, id := range m.visible {
		list = append(list, *m.repo(id))
	}
	return list, 0
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
