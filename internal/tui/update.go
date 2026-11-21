package tui

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mmo-con/git-dashboard/internal/app"
	"github.com/mmo-con/git-dashboard/internal/domain"
	"github.com/mmo-con/git-dashboard/internal/jobs"
	"github.com/mmo-con/git-dashboard/internal/launcher"
)

// Toast durations for informational and error messages.
const (
	toastInfo  = 4 * time.Second
	toastError = 10 * time.Second
	spinEvery  = 100 * time.Millisecond
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.update(msg)
	return m, tea.Batch(cmd, m.housekeeping())
}

// housekeeping starts the spinner while work is pending and schedules the
// expiry of the current message.
func (m *Model) housekeeping() tea.Cmd {
	var cmds []tea.Cmd
	if m.busy() && !m.spinning {
		m.spinning = true
		cmds = append(cmds, tea.Tick(spinEvery, func(time.Time) tea.Msg { return spinMsg{} }))
	}
	if m.message != "" && m.msgTimer != m.msgSeq {
		m.msgTimer = m.msgSeq
		seq, d := m.msgSeq, toastInfo
		if m.messageErr {
			d = toastError
		}
		cmds = append(cmds, tea.Tick(d, func(time.Time) tea.Msg { return toastExpiredMsg{seq: seq} }))
	}
	return tea.Batch(cmds...)
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinMsg:
		m.frame++
		if m.busy() {
			return m, tea.Tick(spinEvery, func(time.Time) tea.Msg { return spinMsg{} })
		}
		m.spinning = false
		return m, nil
	case toastExpiredMsg:
		if msg.seq == m.msgSeq {
			m.message = ""
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.adjustRepoViewport()
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
	if msg.loaded != nil {
		// A changed default view takes effect; otherwise the view the user
		// switched to with v is kept.
		if view := msg.loaded.Config.UI.View; view != m.app.Config.UI.View {
			m.tree = view == "tree"
		}
		m.app.Activate(msg.loaded)
	}
	m.theme = newTheme(m.app.Config.UI.Theme)
	m.icons = iconSet(m.app.Config.UI.Icons)
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
	m.inspecting = map[uint64]int{}
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
			pending, current := m.inspecting[ev.BatchID]
			if !current || !res.Status.Final() {
				continue
			}
			if pending > 1 {
				m.inspecting[ev.BatchID] = pending - 1
			} else {
				delete(m.inspecting, ev.BatchID)
			}
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
		return m.quit()
	}
	switch m.mode {
	case modeConfirmQuit:
		return m.keyConfirmQuit(key)
	case modeFilter:
		return m.keyFilter(msg)
	case modeFamily:
		m.keyFamily(msg)
		return nil
	case modeHelp:
		switch key {
		case "j", "down":
			m.overlayIdx++
		case "k", "up":
			m.overlayIdx = max(m.overlayIdx-1, 0)
		default:
			m.mode = modeNormal
		}
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

func (m *Model) keyNormal(key string) tea.Cmd {
	switch key {
	case "q":
		return m.quit()
	case "j", "down":
		m.moveRepoCursor(m.cursor + 1)
	case "k", "up":
		m.moveRepoCursor(m.cursor - 1)
	case "g", "home":
		m.moveRepoCursor(0)
	case "G", "end":
		m.moveRepoCursor(len(m.rows) - 1)
	case "pgdown":
		m.moveRepoCursor(m.cursor + m.pageSize())
	case "pgup":
		m.moveRepoCursor(m.cursor - m.pageSize())
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
	case "t":
		m.mode, m.overlayIdx, m.familyQuery = modeFamily, m.familyIdx, ""
	case "v":
		m.tree = !m.tree
		m.recompute()
	case "s":
		m.mode, m.overlayIdx = modeSort, int(m.sortKey)
	case "r":
		m.refresh(m.visible)
		m.setMessage(fmt.Sprintf("refreshing %d repositories", len(m.visible)))
	case "R":
		if m.loadingInventory {
			m.setMessage("the inventory is still loading")
			return nil
		}
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
		m.mode, m.overlayIdx = modeHelp, 0
	case "1", "2", "3", "4", "5", "6":
		q := m.quickFilters()[key[0]-'1']
		if strings.TrimSpace(m.filter.Text) == q.token {
			m.filter.Text = ""
		} else {
			m.filter.Text = q.token
		}
		m.recompute()
	case "enter":
		if row := m.currentRow(); row.count > 0 {
			m.expanded[row.family] = !m.expanded[row.family]
			m.recompute()
			return nil
		}
		return m.openLazygit(m.current())
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
			m.moveRepoCursor(m.cursor + 1)
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
		m.previewReturn = modeNormal
		m.mode, m.overlayIdx = modeUpdatePreview, 0
	}
	return nil
}

// quit ends the program. While a fetch or update batch runs, it asks first,
// because quitting cancels the batch.
func (m *Model) quit() tea.Cmd {
	if m.mode == modeConfirmQuit || !m.batchRunning() {
		return tea.Quit
	}
	m.quitReturn = m.mode
	m.mode = modeConfirmQuit
	return nil
}

func (m *Model) keyConfirmQuit(key string) tea.Cmd {
	switch key {
	case "enter", "y", "q":
		return tea.Quit
	case "esc", "n":
		m.mode = m.quitReturn
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
	default:
		if !editQuery(&m.filter.Text, msg) {
			return nil
		}
	}
	m.recompute()
	return nil
}

func editQuery(query *string, msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyBackspace:
		if r := []rune(*query); len(r) > 0 {
			*query = string(r[:len(r)-1])
		}
	case tea.KeyCtrlU:
		*query = ""
	case tea.KeySpace:
		*query += " "
	case tea.KeyRunes:
		*query += string(msg.Runes)
	default:
		return false
	}
	return true
}

func (m *Model) keyFamily(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = modeNormal
	case tea.KeyEnter:
		choices := m.familyChoices()
		if len(choices) > 0 {
			m.familyIdx = choices[m.overlayIdx]
			m.recompute()
			m.mode = modeNormal
		}
	case tea.KeyUp, tea.KeyDown:
		m.moveOverlay(msg.String(), len(m.familyChoices()))
	default:
		if editQuery(&m.familyQuery, msg) {
			m.overlayIdx = 0
		}
	}
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
	case "enter":
		if m.batch == nil || m.overlayIdx >= len(m.batch.rows) {
			return nil
		}
		cmd := m.openLazygit(m.repo(m.batch.rows[m.overlayIdx].job.RepositoryID))
		if cmd != nil {
			m.visit = &visit{batch: m.batch, row: m.overlayIdx}
		}
		return cmd
	case "u":
		m.continueUpdate()
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
		var open []app.UpdateCandidate
		for _, c := range m.updateSkip {
			if c.SkipReason != app.ReasonUpToDate {
				open = append(open, c)
			}
		}
		if len(m.updateOK) == 0 && len(open) == 0 {
			m.mode = modeNormal
			m.setMessage("no eligible repositories")
			return nil
		}
		if m.batch != nil && !m.batch.done() {
			m.setError("another batch is still running")
			return nil
		}
		js := app.UpdateJobs(m.app.Git, m.repos, m.updateOK, m.app.Config.Git.FetchTimeout)
		m.submitBatch(domain.OperationUpdate, js, open)
	case "esc", "n", "q":
		m.mode = m.previewReturn
	}
	return nil
}

// continueUpdate opens a new update preview for every repository of the
// finished update batch that has not been updated.
func (m *Model) continueUpdate() {
	if m.batch == nil || m.batch.op != domain.OperationUpdate || !m.batch.done() {
		m.setError("continue is available for a finished update batch")
		return
	}
	var targets []domain.Repository
	seen := map[domain.RepositoryID]bool{}
	for _, row := range m.batch.rows {
		id := row.job.RepositoryID
		if row.result.Status == domain.StatusSucceeded || seen[id] {
			continue
		}
		if r := m.repo(id); r != nil {
			seen[id] = true
			targets = append(targets, *r)
		}
	}
	if len(targets) == 0 {
		m.setMessage("nothing left to update")
		return
	}
	m.fetchHidden = 0
	m.updateOK, m.updateSkip = app.PlanUpdate(targets, func(s domain.StoreID) bool { return m.storeBusy(s) != "" })
	m.previewReturn = modeOperations
	m.mode, m.overlayIdx = modeUpdatePreview, 0
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
			return m.handover(r, l.Name, l.Command(r.Location.WorktreeRoot, r.WorkspaceDir()))
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
	m.submitBatch(domain.OperationFetch, js, nil)
	return nil
}

// submitBatch starts js. skipped lists repositories that the preview excluded;
// they are shown as skipped rows so that they can be worked through.
func (m *Model) submitBatch(op domain.OperationType, js []jobs.Job, skipped []app.UpdateCandidate) {
	b := &batch{op: op, rows: make([]opRow, len(js), len(js)+len(skipped))}
	for i, j := range js {
		b.rows[i] = opRow{job: j, result: domain.OperationResult{
			RepositoryID: j.RepositoryID, StoreID: j.StoreID, Operation: op, Status: domain.StatusQueued,
		}}
	}
	now := m.now()
	for _, c := range skipped {
		r := c.Repository
		b.rows = append(b.rows, opRow{
			job: jobs.Job{RepositoryID: r.ID, StoreID: r.StoreID, Operation: op},
			result: domain.OperationResult{
				RepositoryID: r.ID, StoreID: r.StoreID, Operation: op, Status: domain.StatusSkipped,
				FinishedAt: now, Error: errors.New(c.SkipReason),
			},
		})
	}
	m.batch = b
	m.mode, m.overlayIdx = modeOperations, 0
	if len(js) == 0 {
		m.setMessage(fmt.Sprintf("nothing to %s; %d blocked repositories listed", op, len(skipped)))
		return
	}
	b.id = m.app.Scheduler.Submit(js)
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
	m.submitBatch(m.batch.op, js, nil)
	return nil
}

func (m *Model) openLazygit(r *domain.Repository) tea.Cmd {
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
	if v := m.visit; v != nil {
		m.visit = nil
		if v.batch == m.batch && v.row < len(m.batch.rows) {
			m.batch.rows[v.row].revisitedAt = m.now()
		}
	}
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
