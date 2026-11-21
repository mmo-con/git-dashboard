package tui

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mmo-con/git-dashboard/internal/app"
	"github.com/mmo-con/git-dashboard/internal/domain"
	"github.com/mmo-con/git-dashboard/internal/jobs"
	"github.com/mmo-con/git-dashboard/internal/launcher"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		return m, tick()
	case inventoryMsg:
		m.applyInventory(msg)
		return m, nil
	case reloadFailedMsg:
		m.loadingInventory = false
		m.setError("configuration not reloaded: " + firstLine(msg.err.Error()))
		m.warnings = append(m.warnings, msg.err.Error())
		return m, nil
	case eventsMsg:
		m.applyEvents(msg.events)
		return m, m.waitEvents()
	case handoverDoneMsg:
		return m, m.finishHandover(msg)
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) applyInventory(msg inventoryMsg) {
	m.loadingInventory = false
	m.repos = msg.result.Repositories
	m.warnings = msg.result.Warnings
	m.index = make(map[domain.RepositoryID]int, len(m.repos))
	for i, r := range m.repos {
		m.index[r.ID] = i
	}
	for id := range m.marks {
		if _, ok := m.index[id]; !ok {
			delete(m.marks, id)
		}
	}
	m.rebuildFamilies()
	m.recompute()
	m.pendingInspect = 0
	m.refresh(m.allIDs())
	if len(m.warnings) > 0 {
		m.setError(fmt.Sprintf("%d configuration warning(s); press e for details", len(m.warnings)))
	}
}

func (m *Model) applyEvents(events []jobs.Event) {
	now := m.now()
	reinspect := map[domain.RepositoryID]bool{}
	for _, ev := range events {
		res := ev.Result
		if res.Operation == "" {
			if !res.Status.Final() {
				continue
			}
			m.pendingInspect = max(m.pendingInspect-1, 0)
			if p, ok := ev.Payload.(app.InspectionPayload); ok {
				if r := m.repo(res.RepositoryID); r != nil {
					r.ApplyInspection(p.Generation, p.Inspection, p.Err, now)
				}
			}
			continue
		}
		if m.batch == nil || ev.BatchID != m.batch.id || ev.JobID >= len(m.batch.rows) {
			continue
		}
		m.batch.rows[ev.JobID].result = res
		if res.Status.Final() {
			for _, id := range ev.Repositories {
				m.lastOps[id] = res
				reinspect[id] = true
			}
		}
	}
	if m.batch != nil && len(reinspect) > 0 && m.batch.done() {
		m.summarizeBatch()
	}
	if len(reinspect) > 0 {
		ids := make([]domain.RepositoryID, 0, len(reinspect))
		for id := range reinspect {
			ids = append(ids, id)
		}
		m.refresh(ids)
	}
	app.RefreshStoreFetchTimes(m.repos)
	m.recompute()
}

func (m *Model) summarizeBatch() {
	counts := map[domain.OperationStatus]int{}
	for _, r := range m.batch.rows {
		counts[r.result.Status]++
	}
	msg := fmt.Sprintf("%s finished: %d succeeded", m.batch.op, counts[domain.StatusSucceeded])
	for _, s := range []domain.OperationStatus{domain.StatusFailed, domain.StatusSkipped, domain.StatusCancelled} {
		if counts[s] > 0 {
			msg += fmt.Sprintf(", %d %s", counts[s], s)
		}
	}
	if counts[domain.StatusFailed] > 0 {
		m.setError(msg + " (press b for details)")
	} else {
		m.setMessage(msg)
	}
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+c" {
		return tea.Quit
	}
	switch m.mode {
	case modeFilter:
		return m.keyFilter(msg)
	case modeHelp:
		m.mode = modeNormal
		return nil
	case modeError:
		return m.keyError(key)
	case modeSort:
		return m.keySort(key)
	case modeOperations:
		return m.keyOperations(key)
	case modeConfirmFetch:
		return m.keyConfirmFetch(key)
	case modeUpdatePreview:
		return m.keyUpdatePreview(key)
	case modeLaunchers:
		return m.keyLaunchers(key)
	}
	return m.keyNormal(key)
}

func (m *Model) pageSize() int { return max(m.tableHeight()-1, 1) }

func (m *Model) keyNormal(key string) tea.Cmd {
	switch key {
	case "q":
		return tea.Quit
	case "j", "down":
		m.cursor = min(m.cursor+1, max(len(m.visible)-1, 0))
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = max(len(m.visible)-1, 0)
	case "pgdown":
		m.cursor = min(m.cursor+m.pageSize(), max(len(m.visible)-1, 0))
	case "pgup":
		m.cursor = max(m.cursor-m.pageSize(), 0)
	case "/":
		m.mode = modeFilter
	case "esc":
		if m.filter.Text != "" {
			m.filter.Text = ""
			m.recompute()
		}
	case "tab":
		m.familyIdx = (m.familyIdx + 1) % (len(m.families) + 1)
		m.recompute()
	case "shift+tab":
		m.familyIdx = (m.familyIdx + len(m.families)) % (len(m.families) + 1)
		m.recompute()
	case "s":
		m.mode, m.overlayIdx = modeSort, int(m.sortKey)
	case "r":
		m.refresh(m.visible)
		m.setMessage(fmt.Sprintf("refreshing %d repositories", len(m.visible)))
	case "R":
		m.loadingInventory = true
		m.setMessage("reloading configuration and rebuilding inventory")
		return m.reload()
	case "e":
		m.mode, m.overlayIdx = modeError, 0
	case "b":
		if m.batch != nil {
			m.mode, m.overlayIdx = modeOperations, 0
		} else {
			m.setMessage("no operations yet")
		}
	case "?":
		m.mode = modeHelp
	case "enter":
		return m.openLazygit()
	case "o":
		if m.current() != nil {
			m.mode, m.overlayIdx = modeLaunchers, 0
		}
	case " ":
		if r := m.current(); r != nil {
			if m.marks[r.ID] {
				delete(m.marks, r.ID)
			} else {
				m.marks[r.ID] = true
			}
			m.cursor = min(m.cursor+1, max(len(m.visible)-1, 0))
		}
	case "a":
		for _, id := range m.visible {
			m.marks[id] = true
		}
	case "A":
		m.marks = map[domain.RepositoryID]bool{}
		m.recompute()
	case "f":
		if len(m.marks) > 0 {
			m.fetchTargets, m.fetchHidden = m.targets()
			m.mode = modeConfirmFetch
		} else if r := m.current(); r != nil {
			return m.startFetch([]domain.Repository{*r})
		}
	case "F":
		m.fetchTargets, m.fetchHidden = nil, 0
		for _, id := range m.visible {
			m.fetchTargets = append(m.fetchTargets, *m.repo(id))
		}
		m.mode = modeConfirmFetch
	case "u":
		targets, hidden := m.targets()
		m.fetchHidden = hidden
		m.updateOK, m.updateSkip = app.PlanUpdate(targets, func(s domain.StoreID) bool { return m.storeBusy(s) != "" })
		m.mode, m.overlayIdx = modeUpdatePreview, 0
	}
	return nil
}

func (m *Model) keyFilter(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEnter:
		m.mode = modeNormal
	case tea.KeyEsc:
		m.filter.Text = ""
		m.mode = modeNormal
	case tea.KeyBackspace:
		if r := []rune(m.filter.Text); len(r) > 0 {
			m.filter.Text = string(r[:len(r)-1])
		}
	case tea.KeyCtrlU:
		m.filter.Text = ""
	case tea.KeySpace:
		m.filter.Text += " "
	case tea.KeyRunes:
		m.filter.Text += string(msg.Runes)
	default:
		return nil
	}
	m.recompute()
	return nil
}

func (m *Model) moveOverlay(key string, n int) bool {
	switch key {
	case "j", "down":
		m.overlayIdx = min(m.overlayIdx+1, max(n-1, 0))
	case "k", "up":
		m.overlayIdx = max(m.overlayIdx-1, 0)
	default:
		return false
	}
	return true
}

func (m *Model) keySort(key string) tea.Cmd {
	if m.moveOverlay(key, len(app.SortKeys)) {
		return nil
	}
	switch key {
	case "enter":
		m.sortKey = app.SortKeys[m.overlayIdx]
		m.recompute()
		m.mode = modeNormal
	case "esc", "q", "s":
		m.mode = modeNormal
	}
	return nil
}

func (m *Model) keyError(key string) tea.Cmd {
	switch key {
	case "esc", "q", "e":
		m.mode = modeNormal
	case "t":
		if r := m.current(); r != nil {
			m.mode = modeNormal
			return m.interactiveFetch(*r)
		}
	}
	return nil
}

func (m *Model) keyOperations(key string) tea.Cmd {
	n := 0
	if m.batch != nil {
		n = len(m.batch.rows)
	}
	if m.moveOverlay(key, n) {
		return nil
	}
	switch key {
	case "esc", "q", "b":
		m.mode = modeNormal
	case "c":
		if m.batch != nil && !m.batch.done() {
			m.app.Scheduler.Cancel(m.batch.id)
			m.setMessage("cancelling batch")
		}
	case "x":
		return m.retryFailed()
	case "t":
		if m.batch == nil || m.overlayIdx >= len(m.batch.rows) {
			return nil
		}
		row := m.batch.rows[m.overlayIdx]
		if row.job.Operation != domain.OperationFetch || row.result.Status != domain.StatusFailed {
			m.setError("select a failed fetch to retry it in the terminal")
			return nil
		}
		if r := m.repo(row.job.RepositoryID); r != nil {
			return m.interactiveFetch(*r)
		}
	}
	return nil
}

func (m *Model) keyConfirmFetch(key string) tea.Cmd {
	switch key {
	case "enter", "y":
		m.mode = modeNormal
		return m.startFetch(m.fetchTargets)
	case "esc", "n", "q":
		m.mode = modeNormal
	}
	return nil
}

func (m *Model) keyUpdatePreview(key string) tea.Cmd {
	if m.moveOverlay(key, len(m.updateOK)+len(m.updateSkip)) {
		return nil
	}
	switch key {
	case "enter", "y":
		if len(m.updateOK) == 0 {
			m.mode = modeNormal
			m.setMessage("no eligible repositories")
			return nil
		}
		if m.batch != nil && !m.batch.done() {
			m.setError("another batch is still running")
			return nil
		}
		js := app.UpdateJobs(m.app.Git, m.repos, m.updateOK, m.app.Config.Git.FetchTimeout)
		m.submitBatch(domain.OperationUpdate, js)
	case "esc", "n", "q":
		m.mode = modeNormal
	}
	return nil
}

type launcherItem struct {
	label string
	run   func(r domain.Repository) tea.Cmd
}

func (m *Model) launcherItems() []launcherItem {
	var items []launcherItem
	for _, l := range m.app.Launchers {
		l := l
		items = append(items, launcherItem{label: l.Name, run: func(r domain.Repository) tea.Cmd {
			if l.Err != nil {
				m.setError(fmt.Sprintf("launcher %s: %v", l.Name, l.Err))
				return nil
			}
			return m.handover(r, l.Name, l.Command(r.Location.WorktreeRoot))
		}})
	}
	items = append(items, launcherItem{label: "Open remote in browser", run: func(r domain.Repository) tea.Cmd {
		if r.PrimaryRemote == nil {
			m.setError("no remote configured")
			return nil
		}
		u, ok := launcher.BrowserURL(r.PrimaryRemote.URL)
		if !ok {
			m.setError("cannot derive an HTTPS URL from " + domain.RedactURL(r.PrimaryRemote.URL))
			return nil
		}
		if err := launcher.OpenBrowser(u); err != nil {
			m.setError(err.Error())
			return nil
		}
		m.setMessage("opened " + u)
		return nil
	}})
	return items
}

func (m *Model) keyLaunchers(key string) tea.Cmd {
	items := m.launcherItems()
	if m.moveOverlay(key, len(items)) {
		return nil
	}
	switch key {
	case "enter":
		m.mode = modeNormal
		r := m.current()
		if r == nil || m.overlayIdx >= len(items) {
			return nil
		}
		if !r.IsAvailable() {
			m.setError(r.Name + " is " + r.Availability.String())
			return nil
		}
		return items[m.overlayIdx].run(*r)
	case "esc", "q", "o":
		m.mode = modeNormal
	}
	return nil
}

func (m *Model) startFetch(targets []domain.Repository) tea.Cmd {
	if m.batch != nil && !m.batch.done() {
		m.setError("another batch is still running")
		return nil
	}
	js := app.FetchJobs(m.app.Git, m.repos, targets, m.app.FetchOptions(), m.app.Config.Git.FetchTimeout)
	if len(js) == 0 {
		m.setError("no available repositories to fetch")
		return nil
	}
	m.submitBatch(domain.OperationFetch, js)
	return nil
}

func (m *Model) submitBatch(op domain.OperationType, js []jobs.Job) {
	b := &batch{op: op, rows: make([]opRow, len(js))}
	for i, j := range js {
		b.rows[i] = opRow{job: j, result: domain.OperationResult{
			RepositoryID: j.RepositoryID, StoreID: j.StoreID, Operation: op, Status: domain.StatusQueued,
		}}
	}
	m.batch = b
	b.id = m.app.Scheduler.Submit(js)
	m.mode, m.overlayIdx = modeOperations, 0
	m.setMessage(fmt.Sprintf("%s started for %d repositories", op, len(js)))
}

func (m *Model) retryFailed() tea.Cmd {
	if m.batch == nil || !m.batch.done() {
		return nil
	}
	var js []jobs.Job
	for _, r := range m.batch.rows {
		if r.result.Status == domain.StatusFailed || r.result.Status == domain.StatusCancelled {
			js = append(js, r.job)
		}
	}
	if len(js) == 0 {
		m.setMessage("nothing to retry")
		return nil
	}
	m.submitBatch(m.batch.op, js)
	return nil
}

func (m *Model) openLazygit() tea.Cmd {
	r := m.current()
	if r == nil {
		return nil
	}
	if !r.IsAvailable() {
		m.setError(r.Name + " is " + r.Availability.String())
		return nil
	}
	if !m.app.Lazygit.Available() {
		m.setError(m.app.Lazygit.Err.Error())
		return nil
	}
	return m.handover(*r, "lazygit", m.app.Lazygit.Command(r.Location.WorktreeRoot))
}

func (m *Model) interactiveFetch(r domain.Repository) tea.Cmd {
	if !r.IsAvailable() {
		m.setError(r.Name + " is " + r.Availability.String())
		return nil
	}
	return m.handover(r, "git fetch", m.app.Git.InteractiveFetchCommand(r.Location, m.app.FetchOptions()))
}

// handover gives the terminal to cmd. The store is locked so that no job runs
// for it while the child process owns the terminal.
func (m *Model) handover(r domain.Repository, label string, cmd *exec.Cmd) tea.Cmd {
	if busy := m.storeBusy(r.StoreID); busy != "" || !m.app.Scheduler.Locks.TryLock(r.StoreID) {
		if busy == "" {
			busy = "another operation"
		}
		m.setError(fmt.Sprintf("%s in progress for %s", busy, r.Name))
		return nil
	}
	m.handovers[r.StoreID] = true
	repo, store := r.ID, r.StoreID
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		var exitErr *exec.ExitError
		fatal := err != nil && cmd.ProcessState != nil && !errors.As(err, &exitErr)
		return handoverDoneMsg{label: label, repo: repo, store: store, err: err, fatal: fatal}
	})
}

func (m *Model) finishHandover(msg handoverDoneMsg) tea.Cmd {
	m.app.Scheduler.Locks.Unlock(msg.store)
	delete(m.handovers, msg.store)
	if msg.fatal {
		m.fatal = fmt.Errorf("restoring the terminal after %s failed: %w", msg.label, msg.err)
		return tea.Quit
	}
	if msg.err != nil {
		m.setError(fmt.Sprintf("%s: %v", msg.label, msg.err))
	} else {
		m.setMessage(msg.label + " finished")
		if msg.label == "git fetch" {
			for _, id := range app.StoreMembers(m.repos, map[domain.StoreID]bool{msg.store: true}) {
				delete(m.lastOps, id)
			}
		}
	}
	m.refresh(app.StoreMembers(m.repos, map[domain.StoreID]bool{msg.store: true}))
	return nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
