package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mmo-con/git-dashboard/internal/app"
	"github.com/mmo-con/git-dashboard/internal/config"
	"github.com/mmo-con/git-dashboard/internal/domain"
	"github.com/mmo-con/git-dashboard/internal/git"
	"github.com/mmo-con/git-dashboard/internal/inventory"
	"github.com/mmo-con/git-dashboard/internal/jobs"
)

func repo(name string, family domain.FamilyID, dirty bool) domain.Repository {
	w := domain.WorktreeState{UntrackedChecked: true}
	if dirty {
		w.Modified = 1
	}
	return domain.Repository{
		ID: domain.RepositoryID("/r/" + name), StoreID: domain.StoreID("/r/" + name + "/.git"),
		Name: name, Path: "/r/" + name, Family: family,
		// Unavailable entries are not inspected, which keeps the test free of
		// Git processes.
		Availability: domain.NotARepository,
		Inspection:   domain.InspectionInfo{FinishedAt: time.Now(), Generation: 1},
		Worktree:     domain.KnownValue(w),
	}
}

func newModel(t *testing.T) *Model {
	t.Helper()
	a := app.New("/none", config.Default(), git.New("git"))
	t.Cleanup(a.Close)
	m := New(a)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.Update(inventoryMsg{result: inventory.Result{Repositories: []domain.Repository{
		repo("api", "Work", true), repo("frontend", "Work", false), repo("dotfiles", "Personal", false),
	}}})
	return m
}

func keys(m *Model, ks ...string) {
	for _, k := range ks {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case " ":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m.Update(msg)
	}
}

// inspectionEvent returns a finished inspection of a batch that belongs to
// the current inventory.
func inspectionEvent(m *Model, id domain.RepositoryID, p app.InspectionPayload) eventsMsg {
	const batchID = 999
	m.inspecting[batchID]++
	return eventsMsg{events: []jobs.Event{{BatchID: batchID,
		Result: domain.OperationResult{RepositoryID: id, Status: domain.StatusSucceeded}, Payload: p}}}
}

func names(m *Model) string {
	var out []string
	for _, id := range m.visible {
		out = append(out, m.repo(id).Name)
	}
	return strings.Join(out, " ")
}

func TestRepoViewportKeepsContextAndLetsCursorMoveBackUp(t *testing.T) {
	m := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 16}) // seven repository rows
	var repos []domain.Repository
	for i := range 24 {
		repos = append(repos, repo(fmt.Sprintf("repo-%02d", i), "Work", false))
	}
	m.Update(inventoryMsg{result: inventory.Result{Repositories: repos}})

	keys(m, "j", "j", "j", "j", "j")
	if m.current().Name != "repo-05" || m.repoTop != 1 || !strings.Contains(m.View(), "repo-07") {
		t.Fatalf("scroll should keep two rows below the cursor; cursor=%d top=%d", m.cursor, m.repoTop)
	}
	keys(m, "k")
	if m.current().Name != "repo-04" || m.repoTop != 1 {
		t.Fatalf("reversing direction must move the cursor within the view; cursor=%d top=%d", m.cursor, m.repoTop)
	}
	keys(m, "G", "k")
	if m.current().Name != "repo-22" || m.repoTop != 17 || !strings.Contains(m.View(), "repo-23") {
		t.Fatalf("moving up from the last repo must keep the last page in view; cursor=%d top=%d", m.cursor, m.repoTop)
	}
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 16}) // eleven repository rows
	if m.current().Name != "repo-22" || m.repoTop != 13 {
		t.Fatalf("resize must preserve selection and fill the view; cursor=%d top=%d", m.cursor, m.repoTop)
	}
	keys(m, "/", "repo-22", "enter")
	if m.repoTop != 0 || names(m) != "repo-22" || !strings.Contains(m.View(), "repo-22") {
		t.Fatalf("filter must reset the viewport for a short list; top=%d, repos=%q", m.repoTop, names(m))
	}
}

func TestFilterFamilyAndSelection(t *testing.T) {
	m := newModel(t)
	if names(m) != "api dotfiles frontend" {
		t.Fatalf("visible = %q", names(m))
	}
	keys(m, "j", "j") // cursor on frontend
	keys(m, "/", "i", "s", ":", "d", "i", "r", "t", "y", "enter")
	if names(m) != "api" || m.mode != modeNormal {
		t.Fatalf("filter: %q", names(m))
	}
	keys(m, "esc")
	if names(m) != "api dotfiles frontend" {
		t.Fatalf("Esc must clear the filter: %q", names(m))
	}
	keys(m, "tab")
	if m.filter.Family != "Personal" || names(m) != "dotfiles" {
		t.Fatalf("family: %q %q", m.filter.Family, names(m))
	}
	keys(m, "tab", "tab")
	if m.filter.Family != "" {
		t.Fatalf("family cycle must return to All: %q", m.filter.Family)
	}
}

func TestFamilyTreeNavigationSortingAndFiltering(t *testing.T) {
	m := newModel(t)
	keys(m, "v")
	view := m.View()
	if names(m) != "" || m.currentRow().family != "Work" ||
		!strings.Contains(view, "▸ Personal (1)") || !strings.Contains(view, "▸ Work (2)") {
		t.Fatal("the tree must start with only collapsed family rows and select the current repo's family")
	}
	keys(m, "g", "enter", "G", "enter")
	if m.current() != nil || names(m) != "dotfiles api frontend" {
		t.Fatal("Enter on a collapsed family must show its repos and keep its header selected")
	}
	keys(m, "enter")
	if names(m) != "dotfiles" || !strings.Contains(m.View(), "▸ Work (2)") {
		t.Fatal("Enter on a family must collapse it and keep the family selected")
	}
	keys(m, "v", "v")
	if m.currentRow().family != "Work" || m.current() != nil || names(m) != "dotfiles" {
		t.Fatal("switching views must retain expansion states and recover the header selection")
	}
	keys(m, "enter", "s", "j", "j", "enter")
	// With no commit data, the sort falls back to name. Give frontend a
	// commit so that latest-commit sorting changes the order within Work.
	m.repo("/r/frontend").LastCommit = domain.KnownValue(&domain.CommitSummary{Time: time.Now()})
	m.recompute()
	if names(m) != "dotfiles frontend api" {
		t.Fatalf("sort must apply within each family: %s", names(m))
	}
	keys(m, "/", "front", "enter")
	if names(m) != "frontend" || strings.Contains(m.View(), "Personal") || !strings.Contains(m.View(), "▾ Work (1)") {
		t.Fatal("the tree must show only families with matches and count matching repositories")
	}
	keys(m, "G")
	if m.current().Name != "frontend" {
		t.Fatal("End must reach the last repository even when a header takes up a row")
	}
}

func TestFamilyTreeBatchScope(t *testing.T) {
	m := newModel(t)
	keys(m, " ", "v", "g", "enter") // mark api, then expand only Personal
	keys(m, "f")
	if len(m.fetchTargets) != 1 || m.fetchTargets[0].Name != "api" || m.fetchHidden != 1 {
		t.Fatalf("marked fetch must include collapsed repo: targets=%+v, hidden=%d", m.fetchTargets, m.fetchHidden)
	}
	if !strings.Contains(m.View(), "hidden by filters or collapsed groups") {
		t.Fatal("fetch confirmation must explain hidden marks")
	}
	keys(m, "esc", "F")
	if len(m.fetchTargets) != 1 || m.fetchTargets[0].Name != "dotfiles" || m.fetchHidden != 0 {
		t.Fatal("F must fetch only expanded repos, regardless of marks")
	}
	keys(m, "esc", "u")
	if len(m.updateSkip) != 1 || m.updateSkip[0].Repository.Name != "api" || m.fetchHidden != 1 {
		t.Fatal("the update preview must include explicitly marked collapsed repos")
	}
	keys(m, "esc", "A", "u")
	if len(m.updateSkip) != 1 || m.updateSkip[0].Repository.Name != "dotfiles" || m.fetchHidden != 0 {
		t.Fatal("the update preview without marks must omit collapsed repos")
	}
	keys(m, "esc", "a")
	if len(m.marks) != 1 || !m.marks["/r/dotfiles"] {
		t.Fatal("mark-all must mark only the expanded repos")
	}
}

func TestFamilyPickerCombinesWithFiltersAndPreservesMarks(t *testing.T) {
	m := newModel(t)
	keys(m, "a", "1", "t", "wO")
	if m.mode != modeFamily || !strings.Contains(m.View(), "Search: wO") || m.filter.Family != "" {
		t.Fatal("typing in the picker must show the query without applying a family")
	}
	keys(m, "enter")
	if m.mode != modeNormal || m.filter.Family != "Work" || m.filter.Text != "is:dirty" || names(m) != "api" {
		t.Fatalf("pick Work: family=%q, text=%q, repos=%q", m.filter.Family, m.filter.Text, names(m))
	}
	if _, hidden := m.targets(); len(m.marks) != 3 || hidden != 2 {
		t.Fatalf("picking a family must preserve all marks: marks=%v hidden=%d", m.marks, hidden)
	}
	keys(m, "tab")
	if m.filter.Family != "" || m.filter.Text != "is:dirty" {
		t.Fatalf("Tab must cycle from the chosen family to All without clearing text: %+v", m.filter)
	}
}

func TestFamilyPickerCancelAndClear(t *testing.T) {
	m := newModel(t)
	keys(m, "tab", "t", "does not match", "enter")
	if m.mode != modeFamily || !strings.Contains(m.View(), "No matching families") {
		t.Fatal("Enter with no matches must leave the picker open")
	}
	keys(m, "esc")
	if m.mode != modeNormal || m.filter.Family != "Personal" || names(m) != "dotfiles" {
		t.Fatal("Esc must leave the applied family unchanged")
	}
	keys(m, "t")
	if m.familyQuery != "" {
		t.Fatal("a reopened picker must start with an empty query")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	keys(m, "enter")
	if m.mode != modeNormal || m.filter.Family != "" || names(m) != "api dotfiles frontend" {
		t.Fatal("choosing All families must remove the family filter")
	}
}

func TestMarksTargetsAndHiddenCount(t *testing.T) {
	m := newModel(t)
	keys(m, " ", " ") // mark api and dotfiles
	if len(m.marks) != 2 {
		t.Fatalf("marks = %v", m.marks)
	}
	keys(m, "/", "f", "r", "o", "n", "t", "enter")
	targets, hidden := m.targets()
	if len(targets) != 2 || hidden != 2 {
		t.Fatalf("targets=%d hidden=%d", len(targets), hidden)
	}
	if !strings.Contains(m.View(), "2 marked · A: clear marks") {
		t.Fatal("repository panel must show the count and shortcut even when all marks are hidden")
	}
	keys(m, "f")
	if m.mode != modeConfirmFetch || m.fetchHidden != 2 {
		t.Fatalf("f with marks must open the confirmation: mode=%v hidden=%d", m.mode, m.fetchHidden)
	}
	if !strings.Contains(m.View(), "Includes 2 marked repositories hidden by the filter") {
		t.Fatal("confirmation must mention hidden marks")
	}
	keys(m, "esc", "A")
	if t2, _ := m.targets(); len(t2) != 1 || len(m.marks) != 0 {
		t.Fatalf("without marks the visible rows are targets: %d", len(t2))
	}
	if strings.Contains(m.View(), "A: clear marks") {
		t.Fatal("repository panel must hide the shortcut once marks are cleared")
	}
}

func TestBatchEventsAreApplied(t *testing.T) {
	m := newModel(t)
	r := m.repos[0]
	m.batch = &batch{id: 7, op: domain.OperationFetch, rows: []opRow{{
		job:    jobs.Job{RepositoryID: r.ID, StoreID: r.StoreID, Operation: domain.OperationFetch},
		result: domain.OperationResult{Status: domain.StatusQueued},
	}}}
	if m.storeBusy(r.StoreID) != "fetch" {
		t.Fatal("queued fetch must mark the store busy")
	}
	m.Update(eventsMsg{events: []jobs.Event{{BatchID: 7, JobID: 0, Repositories: []domain.RepositoryID{r.ID},
		Result: domain.OperationResult{RepositoryID: r.ID, Operation: domain.OperationFetch, Status: domain.StatusFailed}}}})
	if !m.batch.done() || m.storeBusy(r.StoreID) != "" {
		t.Fatal("batch must be finished")
	}
	if got := m.upstreamText(m.repo(r.ID)); got != "✗ fetch failed" {
		t.Fatalf("upstream column = %q", got)
	}
	if !m.messageErr {
		t.Fatal("failed batch must be reported as an error")
	}
}

func TestStaleInspectionIsIgnored(t *testing.T) {
	m := newModel(t)
	id := m.repos[0].ID
	m.repo(id).Inspection.Generation = 5
	m.Update(inspectionEvent(m, id, app.InspectionPayload{Generation: 4, Inspection: domain.Inspection{
		Worktree: domain.KnownValue(domain.WorktreeState{Conflicts: 9})}}))
	if m.repo(id).Worktree.Value.Conflicts == 9 {
		t.Fatal("an older generation overwrote newer state")
	}
}

func TestStatusTexts(t *testing.T) {
	m := newModel(t)
	r := repo("x", "", false)
	r.Availability = domain.Available
	r.Head = domain.KnownValue(domain.HeadState{Branch: "main"})
	r.Operation = domain.KnownValue(domain.Merging)
	r.Worktree = domain.KnownValue(domain.WorktreeState{Conflicts: 1, Staged: 2, Modified: 3, Untracked: 4})
	r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true, Ahead: 1, Behind: 2})
	r.ExpectedRemote = &domain.ExpectedRemote{URL: "git@a:b.git"}
	r.Remotes = domain.KnownValue([]domain.Remote{{Name: "origin", URL: "git@a:c.git"}})
	if got := m.statusText(&r); got != "merging  ✘1  ✚2  ●3  ?4  ≠remote" {
		t.Errorf("status = %q", got)
	}
	if got := m.upstreamText(&r); got != "↑1 ↓2 diverged" {
		t.Errorf("upstream = %q", got)
	}
	r.Operation = domain.KnownValue(domain.NoOperation)
	r.ExpectedRemote = nil
	r.Worktree = domain.KnownValue(domain.WorktreeState{UntrackedChecked: true})
	if got := m.statusText(&r); got != "✓" {
		t.Errorf("a clean worktree shows only the symbol, like the upstream column: %q", got)
	}
	r.Worktree = domain.Known[domain.WorktreeState]{}
	if got := m.statusText(&r); got != "? unknown" {
		t.Errorf("unknown state must not be shown as clean: %q", got)
	}
	r.Availability = domain.Missing
	if got := m.statusText(&r); got != "✗ missing" {
		t.Errorf("missing = %q", got)
	}

	m.icons = iconSet("ascii")
	r.Availability = domain.Available
	r.Worktree = domain.KnownValue(domain.WorktreeState{Modified: 1})
	r.Operation = domain.KnownValue(domain.NoOperation)
	r.ExpectedRemote = nil
	if got := m.statusText(&r) + " | " + m.upstreamText(&r); got != "*1 | ^1 v2 diverged" {
		t.Errorf("ascii = %q", got)
	}
}

func TestHealthPrefersTheMostImportantState(t *testing.T) {
	m := newModel(t)
	r := repo("x", "", false)
	r.Availability = domain.Available
	r.Head = domain.KnownValue(domain.HeadState{Branch: "main"})
	r.Operation = domain.KnownValue(domain.NoOperation)
	r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true})
	check := func(want string, wantHealthy bool) {
		t.Helper()
		if s, healthy := m.health(&r); s.text != want || healthy != wantHealthy {
			t.Errorf("health = %q %v, want %q %v", s.text, healthy, want, wantHealthy)
		}
	}
	check("✓", true)
	r.Branches = domain.KnownValue([]domain.BranchState{{Name: "wip"}})
	check("!", false)
	r.Upstream.Value.Ahead = 1
	check("↑", false)
	r.Upstream.Value.Behind = 2
	check("✘", false)
	r.Upstream.Value.Ahead = 0
	check("↓", false)
	r.Worktree.Value.Untracked = 1
	check("●", false)
	r.Availability = domain.Missing
	check("✗", false)
}

func TestSelectedRowKeepsSemanticColorsReadable(t *testing.T) {
	m := newModel(t)
	th := m.theme
	if fg := th.onSelected(th.dim).GetForeground(); fg != th.selectedFg {
		t.Errorf("secondary text must use the selected foreground, got %v", fg)
	}
	if fg := th.onSelected(th.bold).GetForeground(); fg != th.selectedFg {
		t.Errorf("unstyled text must use the selected foreground, got %v", fg)
	}
	if fg := th.onSelected(th.err).GetForeground(); fg != th.err.GetForeground() {
		t.Errorf("semantic colors must be kept, got %v", fg)
	}
}

func TestFooterHintsFollowContext(t *testing.T) {
	m := newModel(t)
	hints := func() string { return strings.Join(m.normalHints(), " ") }
	if h := hints(); !strings.Contains(h, "Enter lazygit") || !strings.Contains(h, "F fetch shown") || strings.Contains(h, "clear") {
		t.Errorf("default hints = %q", h)
	}
	keys(m, " ", "/", "api", "enter")
	if h := hints(); !strings.Contains(h, "f fetch marked") || !strings.Contains(h, "A clear marks") ||
		!strings.Contains(h, "Esc clear filter") || strings.Contains(h, "fetch shown") {
		t.Errorf("hints with marks and filter = %q", h)
	}
	keys(m, "esc", "A", "v")
	if h := hints(); !strings.HasPrefix(h, "Enter expand") {
		t.Errorf("hints on a family row = %q", h)
	}

	m.batch = &batch{op: domain.OperationFetch, rows: []opRow{
		{job: jobs.Job{Operation: domain.OperationFetch}, result: domain.OperationResult{Status: domain.StatusRunning}},
		{job: jobs.Job{Operation: domain.OperationFetch}, result: domain.OperationResult{Status: domain.StatusFailed}},
	}}
	ops := func() string { return strings.Join(m.operationHints(), " ") }
	if h := ops(); !strings.Contains(h, "c cancel") || strings.Contains(h, "retry") {
		t.Errorf("running batch hints = %q", h)
	}
	m.batch.rows[0].result.Status = domain.StatusSucceeded
	m.overlayIdx = 1
	if h := ops(); strings.Contains(h, "cancel") || !strings.Contains(h, "x retry failed") || !strings.Contains(h, "t retry in terminal") {
		t.Errorf("finished batch hints = %q", h)
	}
}

func TestFamilySummaryCountsHealthLevels(t *testing.T) {
	m := newModel(t)
	got := plain(m.familySummary([]healthLevel{healthOK, healthDirty, healthError, healthDirty, healthUnknown}))
	if got != "✗1 error  ●2 dirty" {
		t.Errorf("summary = %q", got)
	}
	if got := plain(m.familySummary([]healthLevel{healthOK, healthOK})); got != "✓ all healthy" {
		t.Errorf("healthy summary = %q", got)
	}
	if got := plain(m.familySummary([]healthLevel{healthOK, healthUnknown})); got != "" {
		t.Errorf("a family that is still inspected must not be called healthy: %q", got)
	}
}

func TestOperationsAreGroupedByStatus(t *testing.T) {
	m := newModel(t)
	row := func(id string, s domain.OperationStatus) opRow {
		return opRow{job: jobs.Job{RepositoryID: domain.RepositoryID(id)}, result: domain.OperationResult{Status: s}}
	}
	m.batch = &batch{op: domain.OperationFetch, rows: []opRow{
		row("/r/api", domain.StatusSucceeded), row("/r/frontend", domain.StatusQueued),
		row("/r/dotfiles", domain.StatusFailed), row("x", domain.StatusRunning),
	}}
	if got := fmt.Sprint(m.opOrder()); got != "[3 2 1 0]" {
		t.Fatalf("order = %s, want running, failed, queued, completed", got)
	}
	keys(m, "b")
	if m.overlayIdx != 3 {
		t.Fatalf("the first shown row must be selected, got %d", m.overlayIdx)
	}
	keys(m, "j", "j")
	if m.overlayIdx != 1 {
		t.Fatalf("j must follow the display order, got %d", m.overlayIdx)
	}
	m.batch.rows[1].result.Status = domain.StatusRunning
	keys(m, "j")
	if m.overlayIdx != 3 {
		t.Fatalf("the selection must stay on its row when it changes group, got %d", m.overlayIdx)
	}
	v := m.View()
	if !strings.Contains(v, "RUNNING 2") || !strings.Contains(v, "COMPLETED 1") || strings.Index(v, "FAILED") > strings.Index(v, "COMPLETED") {
		t.Fatal("groups must be titled and ordered")
	}
}

func TestSourceLabel(t *testing.T) {
	t.Setenv("HOME", "/home/u")
	for _, c := range []struct {
		r    domain.Repository
		want string
	}{
		{domain.Repository{Source: domain.InventorySourceExplicit, DiscoveryRoot: "/home/u/src"}, "configured"},
		{domain.Repository{Source: domain.InventorySourceDiscovered, DiscoveryRoot: "/home/u/src"}, "discovered in ~/src"},
		{domain.Repository{Source: domain.InventorySourceDiscovered, DiscoveryRoot: "/home/user2"}, "discovered in /home/user2"},
	} {
		if got := sourceLabel(&c.r); got != c.want {
			t.Errorf("sourceLabel = %q, want %q", got, c.want)
		}
	}
}

func TestQuickFiltersToggle(t *testing.T) {
	m := newModel(t)
	keys(m, "1")
	if m.filter.Text != "is:dirty" || names(m) != "api" {
		t.Fatalf("quick filter: %q %q", m.filter.Text, names(m))
	}
	if !strings.Contains(m.headerLine(), "dirty 1") {
		t.Fatalf("header must count dirty repositories: %q", m.headerLine())
	}
	keys(m, "1")
	if m.filter.Text != "" {
		t.Fatal("pressing the key again must clear the quick filter")
	}
}

func TestToastExpires(t *testing.T) {
	m := newModel(t)
	m.setMessage("hello")
	m.Update(tickMsg{})
	seq := m.msgSeq
	m.Update(toastExpiredMsg{seq: seq - 1})
	if m.message == "" {
		t.Fatal("an outdated expiry must not hide a newer message")
	}
	m.Update(toastExpiredMsg{seq: seq})
	if m.message != "" {
		t.Fatal("message must expire")
	}
}

func TestUpdateWorklistAfterLazygit(t *testing.T) {
	m := newModel(t)
	now := time.Now().Add(time.Hour)
	m.now = func() time.Time { return now }
	api, frontend := *m.repo("/r/api"), *m.repo("/r/frontend")

	keys(m, "u")
	m.updateOK = nil
	m.updateSkip = []app.UpdateCandidate{
		{Repository: api, SkipReason: "worktree contains local changes"},
		{Repository: frontend, SkipReason: app.ReasonUpToDate},
	}
	keys(m, "enter")
	if m.mode != modeOperations || m.batch == nil || len(m.batch.rows) != 1 || m.batch.rows[0].job.RepositoryID != api.ID {
		t.Fatalf("blocked repositories must form the worklist, up-to-date ones not: mode=%v batch=%+v", m.mode, m.batch)
	}
	if fu, _ := m.followUp(m.batch.rows[0]); fu != "" {
		t.Fatalf("no follow-up before a new inspection: %q", fu)
	}

	// Return from lazygit opened on the row, then a new inspection arrives.
	m.app.Scheduler.Locks.TryLock(api.StoreID)
	m.handovers[api.StoreID] = true
	m.visit = &visit{batch: m.batch, row: 0}
	m.Update(handoverDoneMsg{label: "lazygit", repo: api.ID, store: api.StoreID})
	if m.mode != modeOperations || m.batch.rows[0].revisitedAt.IsZero() {
		t.Fatal("the operations view must stay open and remember the visit")
	}
	inspect := func(w domain.WorktreeState) {
		now = now.Add(time.Minute)
		r := m.repo(api.ID)
		m.Update(inspectionEvent(m, api.ID, app.InspectionPayload{Generation: r.Inspection.Generation + 1, Inspection: domain.Inspection{
			Head:      domain.KnownValue(domain.HeadState{Branch: "main"}),
			Operation: domain.KnownValue(domain.NoOperation),
			Worktree:  domain.KnownValue(w),
			Upstream:  domain.KnownValue(domain.UpstreamState{Configured: true, Behind: 2}),
		}}))
	}
	m.repo(api.ID).Availability = domain.Available
	inspect(domain.WorktreeState{Modified: 1})
	if fu, ok := m.followUp(m.batch.rows[0]); fu != "still blocked: worktree contains local changes" || ok {
		t.Fatalf("follow-up = %q", fu)
	}
	inspect(domain.WorktreeState{})
	if fu, ok := m.followUp(m.batch.rows[0]); fu != "now eligible" || !ok {
		t.Fatalf("follow-up = %q", fu)
	}
	if !strings.Contains(m.View(), "→ now eligible") {
		t.Fatal("operations view must show the follow-up")
	}

	keys(m, "u")
	if m.mode != modeUpdatePreview || len(m.updateOK) != 1 || m.updateOK[0].Repository.ID != api.ID {
		t.Fatalf("continue must open a new preview for open rows: mode=%v ok=%d", m.mode, len(m.updateOK))
	}
	keys(m, "esc")
	if m.mode != modeOperations {
		t.Fatal("cancelling the continued preview must return to the operations view")
	}
}

func TestViewRendersAllModes(t *testing.T) {
	m := newModel(t)
	for _, width := range []int{80, 120, 160} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		for _, k := range []string{"", "?", "e", "s", "u", "o"} {
			if k != "" {
				keys(m, k)
			}
			for i, l := range strings.Split(m.View(), "\n") {
				if w := lipgloss.Width(l); w > width {
					t.Errorf("width %d, mode %q: line %d is %d cells wide", width, k, i, w)
				}
			}
			if v := m.View(); strings.Count(v, "\n") != m.height-1 {
				t.Errorf("width %d, mode %q renders %d lines, want %d", width, k, strings.Count(v, "\n")+1, m.height)
			}
			if k == "" {
				continue
			}
			keys(m, "esc")
			if m.mode != modeNormal {
				t.Errorf("Esc must close mode after %q", k)
			}
		}
	}
}

func TestOtherBranchesMarkerAndDetails(t *testing.T) {
	m := newModel(t)
	r := repo("svc", "Work", false)
	r.Availability = domain.Available
	var bs []domain.BranchState
	bs = append(bs, domain.BranchState{Name: "main", Upstream: "origin/main", Behind: 2, CommitTime: time.Now()})
	if got := plain(m.branchesMarker(&domain.Repository{Availability: domain.Available,
		Branches: domain.KnownValue(bs)})); got != " │ ⎇1" {
		t.Fatalf("marker = %q", got)
	}
	for i := 0; i < 11; i++ {
		bs = append(bs, domain.BranchState{Name: fmt.Sprintf("wip-%d", i), CommitTime: time.Now()})
	}
	r.Branches = domain.KnownValue(bs)
	if r.UnpublishedBranches() != 11 {
		t.Fatalf("unpublished = %d", r.UnpublishedBranches())
	}
	lines := m.otherBranchLines(&r)
	if len(lines) != 12 || !strings.Contains(lines[1], "↓2") || !strings.Contains(lines[11], "+2 more") {
		t.Fatalf("lines = %q", lines)
	}
}

func TestColumnsFollowContent(t *testing.T) {
	// The longest name, "Engine Portal" and a long branch fit without
	// truncation; the remaining space is not given to the name column.
	natural := []int{0, 24, 1, 13, 26, 7, 18, 4}
	cols := columns(140, natural)
	for i, want := range []int{2, 24, 1, 13, 26, 9, 18, 6} {
		if cols[i].width != want {
			t.Errorf("%s: width %d, want %d", cols[i].title, cols[i].width, want)
		}
	}
	// If space is short, the table still fits and keeps every column visible.
	for _, iw := range []int{60, 80, 100} {
		cols := columns(iw, natural)
		if w := tableWidth(cols) - 1; w > iw {
			t.Errorf("iw %d: table width %d", iw, w)
		}
		if cols[colStatus].width < 6 || cols[colUpstream].width < 12 {
			t.Errorf("iw %d: status and upstream shrink last: %+v", iw, cols)
		}
	}
}

func TestConfiguredViewAndTreeMarkers(t *testing.T) {
	cfg := config.Default()
	cfg.UI.View, cfg.UI.Icons = "tree", "ascii"
	a := app.New("/none", cfg, git.New("git"))
	t.Cleanup(a.Close)
	m := New(a)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.Update(inventoryMsg{result: inventory.Result{Repositories: []domain.Repository{repo("api", "Work", false)}}})
	if !m.tree || !strings.Contains(m.View(), "[+] Work (1)") {
		t.Fatal("view = \"tree\" must start in the tree with ASCII markers")
	}
	keys(m, "enter")
	if v := m.View(); !strings.Contains(v, "[-] Work (1)") || !strings.Contains(v, "`- api") {
		t.Fatal("expanded family must use the ASCII markers")
	}

	// A reload keeps the view the user switched to unless the configured
	// default changes.
	keys(m, "v")
	reload := func(view string) {
		c := config.Default()
		c.UI.View = view
		m.Update(inventoryMsg{loaded: &app.Loaded{Config: c, Git: a.Git},
			result: inventory.Result{Repositories: []domain.Repository{repo("api", "Work", false)}}})
	}
	reload("tree")
	if m.tree {
		t.Fatal("an unchanged view setting must not override the user's choice")
	}
	reload("list")
	if m.tree {
		t.Fatal("view = \"list\" must show the list")
	}
	reload("tree")
	if !m.tree || !strings.Contains(m.View(), "▾ Work (1)") {
		t.Fatal("a changed view setting must take effect; the family stays expanded")
	}
}
