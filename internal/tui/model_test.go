package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/mmo-con/headquarters/internal/app"
	"github.com/mmo-con/headquarters/internal/config"
	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/git"
	"github.com/mmo-con/headquarters/internal/inventory"
	"github.com/mmo-con/headquarters/internal/jobs"
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
		Inspected:    domain.InspectionInfo{FinishedAt: time.Now(), Generation: 1},
		Inspection:   domain.Inspection{Worktree: domain.KnownValue(w)},
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
	m.routes[batchID] = m.applyInspection
	return eventsMsg{events: []jobs.Event{{BatchID: batchID,
		Result: domain.OperationResult{RepositoryID: id, Status: domain.StatusSucceeded}, Payload: p}}}
}

// offline replaces the operations of m with ones whose batches never run:
// their jobs are only listed, and setRow or events deliver their results.
func offline(m *Model) {
	next := uint64(1000)
	m.ops = app.NewOperations(m.app, func([]jobs.Job) uint64 {
		next++
		m.routes[next] = m.applyBatchEvent
		return next
	}, func() time.Time { return m.now() })
}

// fetchBatch starts an offline fetch with one row per repository ID; each
// repository has its own store.
func fetchBatch(t *testing.T, m *Model, ids ...domain.RepositoryID) *app.Batch {
	t.Helper()
	offline(m)
	var targets []domain.Repository
	for _, id := range ids {
		targets = append(targets, domain.Repository{ID: id, StoreID: domain.StoreID(id + "/.git"), Availability: domain.Available})
	}
	if n, ok := m.ops.Fetch(m.repos, targets); !ok {
		t.Fatal(n.Text)
	}
	return m.ops.Batch()
}

// setRow records a result for row i of the current batch without inspecting
// the repository again.
func setRow(m *Model, i int, s domain.OperationStatus, err error) {
	b := m.ops.Batch()
	row := b.Rows[i]
	m.ops.Handle(jobs.Event{BatchID: b.ID, JobID: i, Repositories: []domain.RepositoryID{row.Job.RepositoryID},
		Result: domain.OperationResult{RepositoryID: row.Job.RepositoryID, StoreID: row.Job.StoreID,
			Operation: b.Op, Status: s, Error: err}})
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

func TestQuickFilterHeaderMarksActiveFilter(t *testing.T) {
	m := newModel(t)
	if view := m.View(); !strings.Contains(view, "headquarters") {
		t.Fatalf("view lacks application name: %q", view)
	}
	if h := m.headerLine(); !strings.Contains(h, " 1 dirty 1 ") || strings.Contains(h, "[") {
		t.Fatalf("header = %q", h)
	}
	keys(m, "1")
	if h := m.headerLine(); !strings.Contains(h, "[1 dirty 1]") || strings.Contains(h, "[2") {
		t.Fatalf("the active quick filter must be bracketed: %q", h)
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
	if ts, hidden := m.targets(false); len(ts) != 1 || ts[0].Name != "api" || hidden != 1 {
		t.Fatalf("marked targets must include the collapsed repo: targets=%+v, hidden=%d", ts, hidden)
	}
	if ts, hidden := m.targets(true); len(ts) != 1 || ts[0].Name != "dotfiles" || hidden != 0 {
		t.Fatal("F and U must act on expanded repos only, regardless of marks")
	}
	keys(m, "u")
	if len(m.preview.skip) != 1 || m.preview.skip[0].Repository.Name != "api" || m.preview.hidden != 1 {
		t.Fatal("u must include explicitly marked collapsed repos")
	}
	if !strings.Contains(m.View(), "hidden by filters or collapsed groups") {
		t.Fatal("the update confirmation must explain hidden marks")
	}
	keys(m, "esc", "U")
	if len(m.preview.skip) != 1 || m.preview.skip[0].Repository.Name != "dotfiles" || m.preview.hidden != 0 {
		t.Fatal("U must confirm the expanded repos, regardless of marks")
	}
	keys(m, "esc", "A", "u")
	if m.mode != modeNormal || m.message != "no repository selected" {
		t.Fatalf("u without marks on a family row selects nothing: mode=%v message=%q", m.mode, m.message)
	}
	keys(m, "j", "u")
	if len(m.preview.skip) != 1 || m.preview.skip[0].Repository.Name != "dotfiles" {
		t.Fatal("u without marks must confirm the repository under the cursor")
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
	if _, hidden := m.targets(false); len(m.marks) != 3 || hidden != 2 {
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
	targets, hidden := m.targets(false)
	if len(targets) != 2 || hidden != 2 {
		t.Fatalf("targets=%d hidden=%d", len(targets), hidden)
	}
	if !strings.Contains(m.View(), "2 marked · A: clear marks") {
		t.Fatal("repository panel must show the count and shortcut even when all marks are hidden")
	}
	keys(m, "u")
	if m.mode != modeUpdatePreview || m.preview.hidden != 2 {
		t.Fatalf("u with marks must open the confirmation: mode=%v hidden=%d", m.mode, m.preview.hidden)
	}
	if !strings.Contains(m.View(), "Includes 2 marked repositories hidden by the filter") {
		t.Fatal("confirmation must mention hidden marks")
	}
	keys(m, "esc", "A")
	if t2, _ := m.targets(false); len(t2) != 1 || len(m.marks) != 0 {
		t.Fatalf("without marks the cursor row is the target: %d", len(t2))
	}
	if strings.Contains(m.View(), "A: clear marks") {
		t.Fatal("repository panel must hide the shortcut once marks are cleared")
	}
}

func TestBatchEventsAreApplied(t *testing.T) {
	m := newModel(t)
	r := m.repos[0]
	b := fetchBatch(t, m, r.ID)
	if m.ops.StoreActivity(r.StoreID) != "fetch" {
		t.Fatal("queued fetch must mark the store busy")
	}
	m.Update(eventsMsg{events: []jobs.Event{{BatchID: b.ID, JobID: 0, Repositories: []domain.RepositoryID{r.ID},
		Result: domain.OperationResult{RepositoryID: r.ID, Operation: domain.OperationFetch, Status: domain.StatusFailed}}}})
	if !b.Done() || m.ops.StoreBusy(r.StoreID) || m.routes[b.ID] != nil {
		t.Fatal("batch must be finished")
	}
	if got := m.upstreamText(m.repo(r.ID)); got != "✗ fetch failed" {
		t.Fatalf("upstream = %q", got)
	}
	m.repo(r.ID).Availability = domain.Available
	if got := m.stateText(m.repo(r.ID)); !strings.HasPrefix(got, "✗ fetch failed") {
		t.Fatalf("state column = %q", got)
	}
	if !m.messageErr {
		t.Fatal("failed batch must be reported as an error")
	}
}

func TestStaleInspectionIsIgnored(t *testing.T) {
	m := newModel(t)
	id := m.repos[0].ID
	m.repo(id).Inspected.Generation = 5
	m.Update(inspectionEvent(m, id, app.InspectionPayload{Generation: 4, Inspection: domain.Inspection{
		Worktree: domain.KnownValue(domain.WorktreeState{Conflicts: 9})}}))
	if m.repo(id).Worktree.Value.Conflicts == 9 {
		t.Fatal("an older generation overwrote newer state")
	}
}

// countText joins the count cells of r with "|".
func countText(m *Model, r *domain.Repository) string {
	var out []string
	for _, c := range m.countSegs(r) {
		out = append(out, plain(c))
	}
	return strings.Join(out, "|")
}

func TestStatusTexts(t *testing.T) {
	m := newModel(t)
	r := repo("x", "", false)
	r.Availability = domain.Available
	r.Head = domain.KnownValue(domain.HeadState{Branch: "main"})
	r.Operation = domain.KnownValue(domain.Merging)
	r.Worktree = domain.KnownValue(domain.WorktreeState{Conflicts: 1, Staged: 2, Modified: 3, Deleted: 1, Untracked: 4, UntrackedChecked: true})
	r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true, Ahead: 1, Behind: 2})
	r.ExpectedRemote = &domain.ExpectedRemote{URL: "git@a:b.git"}
	r.Remotes = domain.KnownValue([]domain.Remote{{Name: "origin", URL: "git@a:c.git"}})
	r.Branches = domain.KnownValue([]domain.BranchState{{Name: "wip"}})
	r.Stashes = domain.KnownValue(3)
	if got := m.stateText(&r); got != "merging  ✘1 conflicts  diverged  ≠remote  ⎇1" {
		t.Errorf("state = %q", got)
	}
	if got := countText(m, &r); got != "2|4|4|3|1|2" {
		t.Errorf("counts = %q; modified includes deletions", got)
	}
	if got := m.upstreamText(&r); got != "↑1 ↓2 diverged" {
		t.Errorf("upstream = %q", got)
	}

	r.Operation = domain.KnownValue(domain.NoOperation)
	r.ExpectedRemote, r.Branches = nil, domain.Known[[]domain.BranchState]{}
	r.Worktree = domain.KnownValue(domain.WorktreeState{})
	r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true})
	r.Stashes = domain.KnownValue(0)
	if got := m.stateText(&r) + "#" + countText(m, &r); got != "#–|–|off|–|–|–" {
		t.Errorf("a healthy repository has no state, known zeros are dashes, and unchecked untracked files are off: %q", got)
	}
	r.Upstream = domain.KnownValue(domain.UpstreamState{})
	if got := m.stateText(&r) + "#" + countText(m, &r); got != "∅ no upstream#–|–|off|–||" {
		t.Errorf("without an upstream, ahead and behind do not apply: %q", got)
	}
	r.Worktree = domain.Known[domain.WorktreeState]{}
	r.Upstream = domain.Known[domain.UpstreamState]{}
	r.Stashes = domain.Known[int]{}
	if got := m.stateText(&r) + "#" + countText(m, &r); got != "? unknown#?|?|?|?|?|?" {
		t.Errorf("unknown state must not be shown as zero: %q", got)
	}
	r.Availability = domain.Missing
	if got := m.stateText(&r) + "#" + countText(m, &r); got != "✗ missing#|||||" {
		t.Errorf("missing = %q", got)
	}

	m.icons = iconSet("ascii")
	r.Availability = domain.Available
	r.Worktree = domain.KnownValue(domain.WorktreeState{Modified: 1, UntrackedChecked: true})
	r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true, Gone: true})
	r.Stashes = domain.KnownValue(1)
	if got := m.stateText(&r) + "#" + countText(m, &r); got != "- upstream gone#–|1|–|1||" {
		t.Errorf("ascii = %q", got)
	}
}

func TestStashHints(t *testing.T) {
	m := newModel(t)
	for i := range m.repos {
		r := &m.repos[i]
		r.Availability = domain.Available
		r.Head = domain.KnownValue(domain.HeadState{Branch: "main"})
		r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true})
		r.Stashes = domain.KnownValue(0)
	}
	api := m.repo("/r/api")
	api.Stashes = domain.KnownValue(2)
	keys(m, "7")
	if m.filter.Text != "is:stashed" || names(m) != "api" {
		t.Fatalf("7 must show the repositories with stash entries: %q", names(m))
	}
	detail := func() string { return strings.Join(m.detailLines(api, 60).fit(100), "\n") }
	if v := detail(); !strings.Contains(v, "≡2 stash entries") || strings.Contains(v, "shared") {
		t.Fatalf("detail:\n%s", v)
	}
	m.repo("/r/frontend").StoreID = api.StoreID
	if v := detail(); !strings.Contains(v, "≡2 stash entries  shared by all worktrees") {
		t.Fatalf("a store with linked worktrees must say that they share the stash:\n%s", v)
	}
	api.Stashes = domain.Known[int]{}
	if v := detail(); strings.Contains(v, "stash") {
		t.Fatalf("an unknown count must not be reported as a number of stash entries:\n%s", v)
	}
	if got := countText(m, api); got != "–|1|–|?|–|–" {
		t.Fatalf("the table must show an unknown count as ?: %q", got)
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
	r.Stashes = domain.KnownValue(1)
	check("!", false)
	if s, _ := m.health(&r); s.style.GetForeground() != m.theme.warn.GetForeground() {
		t.Error("stash entries are unpublished work and must be highlighted as a warning")
	}
	r.Stashes = domain.KnownValue(0)
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

	fetchBatch(t, m, "a", "b")
	setRow(m, 0, domain.StatusRunning, nil)
	setRow(m, 1, domain.StatusFailed, nil)
	ops := func() string { return strings.Join(m.operationHints(), " ") }
	if h := ops(); !strings.Contains(h, "c cancel") || strings.Contains(h, "retry") {
		t.Errorf("running batch hints = %q", h)
	}
	setRow(m, 0, domain.StatusSucceeded, nil)
	m.opSelected = 1
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
	fetchBatch(t, m, "/r/api", "/r/frontend", "/r/dotfiles", "x")
	setRow(m, 0, domain.StatusSucceeded, nil)
	setRow(m, 2, domain.StatusFailed, nil)
	setRow(m, 3, domain.StatusRunning, nil)
	if got := fmt.Sprint(m.opOrder()); got != "[3 2 1 0]" {
		t.Fatalf("order = %s, want running, failed, queued, completed", got)
	}
	keys(m, "b")
	if m.opSelected != 3 {
		t.Fatalf("the first shown row must be selected, got %d", m.opSelected)
	}
	keys(m, "j", "j")
	if m.opSelected != 1 {
		t.Fatalf("j must follow the display order, got %d", m.opSelected)
	}
	setRow(m, 1, domain.StatusRunning, nil)
	keys(m, "j")
	if m.opSelected != 3 {
		t.Fatalf("the selection must stay on its row when it changes group, got %d", m.opSelected)
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
	for _, id := range []domain.RepositoryID{"/r/api", "/r/frontend"} {
		r := m.repo(id)
		r.Availability = domain.Available
		r.Head = domain.KnownValue(domain.HeadState{Branch: "main"})
		r.Operation = domain.KnownValue(domain.NoOperation)
		r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true})
	}
	api := *m.repo("/r/api")

	offline(m)
	m.preview = &updatePreview{targets: []domain.RepositoryID{"/r/api", "/r/frontend"}}
	m.startUpdate()
	setRow(m, 0, domain.StatusSucceeded, nil)
	setRow(m, 1, domain.StatusSucceeded, nil)
	m.advanceUpdate()
	b := m.ops.Batch()
	if m.mode != modeOperations || b.Op != domain.OperationUpdate || len(b.Rows) != 1 || b.Rows[0].Job.RepositoryID != api.ID {
		t.Fatalf("blocked repositories must form the worklist, up-to-date ones not: mode=%v batch=%+v", m.mode, b)
	}
	if fu, _ := m.followUp(b.Rows[0]); fu != "" {
		t.Fatalf("no follow-up before a new inspection: %q", fu)
	}

	// Return from lazygit opened on the row, then a new inspection arrives.
	m.ops.BeginHandover(api.StoreID)
	m.visit = &visit{batch: b, row: 0}
	m.Update(handoverDoneMsg{label: "lazygit", repo: api.ID, store: api.StoreID})
	if m.mode != modeOperations || b.Rows[0].RevisitedAt.IsZero() {
		t.Fatal("the operations view must stay open and remember the visit")
	}
	inspect := func(w domain.WorktreeState) {
		now = now.Add(time.Minute)
		r := m.repo(api.ID)
		m.Update(inspectionEvent(m, api.ID, app.InspectionPayload{Generation: r.Inspected.Generation + 1, Inspection: domain.Inspection{
			Head:      domain.KnownValue(domain.HeadState{Branch: "main"}),
			Operation: domain.KnownValue(domain.NoOperation),
			Worktree:  domain.KnownValue(w),
			Upstream:  domain.KnownValue(domain.UpstreamState{Configured: true, CommitID: "up1", Behind: 2}),
		}}))
	}
	m.repo(api.ID).Availability = domain.Available
	inspect(domain.WorktreeState{Modified: 1})
	if fu, ok := m.followUp(b.Rows[0]); fu != "still blocked: worktree contains local changes" || ok {
		t.Fatalf("follow-up = %q", fu)
	}
	inspect(domain.WorktreeState{})
	if fu, ok := m.followUp(b.Rows[0]); fu != "now eligible" || !ok {
		t.Fatalf("follow-up = %q", fu)
	}
	if !strings.Contains(m.View(), "→ now eligible") {
		t.Fatal("operations view must show the follow-up")
	}

	keys(m, "u")
	if m.mode != modeUpdatePreview || len(m.preview.ok) != 1 || m.preview.ok[0].Repository.ID != api.ID {
		t.Fatalf("continue must open a new preview for open rows: mode=%v ok=%d", m.mode, len(m.preview.ok))
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
		Inspection: domain.Inspection{Branches: domain.KnownValue(bs)}})); got != "⎇1" {
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
	if len(lines) != 12 || !strings.Contains(lines[0], "↓2") {
		t.Fatalf("lines = %q", lines)
	}
	if v := strings.Join(m.detailLines(&r, 60).fit(100), "\n"); !strings.Contains(v, "wip-8") ||
		strings.Contains(v, "wip-9") || !strings.Contains(v, "+2 more") {
		t.Fatalf("the detail panel lists at most %d other branches:\n%s", maxBranchLines, v)
	}
}

// TestSmallDetailPanelShortensOtherBranchesFirst checks that many other
// branches do not push the worktree state or an error out of the panel.
func TestDetailSectionsAndCommitLine(t *testing.T) {
	m := newModel(t)
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	r := repo("svc", "Work", false)
	r.Availability = domain.Available
	r.Head = domain.KnownValue(domain.HeadState{Branch: "main"})
	r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true, Ref: "origin/main"})
	r.LastCommit = domain.KnownValue(&domain.CommitSummary{ShortID: "c28633b", Author: "Sam",
		Time: now.Add(-5 * time.Hour), Subject: "Show delivery estimate"})
	lines := m.detailLines(&r, 40).fit(100)
	v := strings.Join(lines, "\n")
	if !strings.Contains(v, "c28633b  Nov 22 07:00  Show delivery estimate") || strings.Contains(v, "Sam") {
		t.Fatalf("the commit must be one line of hash, date, and subject:\n%s", v)
	}
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, "── BRANCH ─") {
			found = true
			if w := lipgloss.Width(l); w != 40 {
				t.Errorf("a section rule must span the panel: width %d, want 40", w)
			}
		}
	}
	if !found {
		t.Fatalf("sections must be titled within a rule:\n%s", v)
	}

	for _, c := range []struct {
		d           time.Duration
		long, short string
	}{
		{5 * time.Minute, "Nov 22 11:55", "Nov 22"},
		{20 * 24 * time.Hour, "Nov  2 12:00", "Nov  2"},
		{400 * 24 * time.Hour, "Oct 18 2025 12:00", "2025"},
	} {
		if got := commitTime(now, now.Add(-c.d), 0); got != c.long {
			t.Errorf("commitTime(-%v) = %q, want %q", c.d, got, c.long)
		}
		if got := commitTime(now, now.Add(-c.d), 6); got != c.short {
			t.Errorf("commitTime(-%v, 6) = %q, want %q", c.d, got, c.short)
		}
	}

	// A commit column too narrow for the full time shows the date only
	// instead of a truncated time.
	m.repos[0].LastCommit = domain.KnownValue(&domain.CommitSummary{Time: now.Add(-400 * 24 * time.Hour)})
	if v := ansi.Strip(strings.Join(m.reposBox(m.measureTable(), 160, 10, true), "\n")); !strings.Contains(v, "Oct 18 2025 12:00") {
		t.Errorf("a wide table shows the full time:\n%s", v)
	}
	if v := ansi.Strip(strings.Join(m.reposBox(m.measureTable(), 70, 10, true), "\n")); !strings.Contains(v, " 2025") || strings.Contains(v, "2025…") {
		t.Errorf("a narrow table shows the date only:\n%s", v)
	}
}

func TestSmallDetailPanelShortensOtherBranchesFirst(t *testing.T) {
	m := newModel(t)
	r := &m.repos[m.index[m.current().ID]]
	r.Availability = domain.Available
	r.Head = domain.KnownValue(domain.HeadState{Branch: "main"})
	r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true})
	r.Operation = domain.KnownValue(domain.Merging)
	var bs []domain.BranchState
	for i := 0; i < 10; i++ {
		bs = append(bs, domain.BranchState{Name: fmt.Sprintf("wip-%d", i), CommitTime: time.Now()})
	}
	r.Branches = domain.KnownValue(bs)
	fetchBatch(t, m, r.ID)
	setRow(m, 0, domain.StatusFailed, errors.New("offline"))
	v := strings.Join(m.detailsBox(120, 12), "\n")
	for _, want := range []string{"merging in progress", "offline", "wip-0", "more"} {
		if !strings.Contains(v, want) {
			t.Errorf("a small detail panel must show %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "wip-9") {
		t.Errorf("the other branches must be shortened:\n%s", v)
	}
}

// TestUpdateAfterFailedFetchSkipsStore checks that a failed fetch is the
// skip reason even if the old state looks up to date, so the repository
// stays on the worklist, and that the update starts without opening a view.
func TestUpdateAfterFailedFetchSkipsStore(t *testing.T) {
	m := newModel(t)
	r := &m.repos[0]
	r.Availability = domain.Available
	r.Head = domain.KnownValue(domain.HeadState{Branch: "main"})
	r.Operation = domain.KnownValue(domain.NoOperation)
	r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true})
	r.Worktree.Value.Modified = 0
	offline(m)
	m.preview = &updatePreview{targets: []domain.RepositoryID{r.ID}}
	m.startUpdate()
	m.mode = modeNormal // the user closed the operations view during the fetch
	setRow(m, 0, domain.StatusFailed, errors.New("remote unavailable"))
	m.advanceUpdate()
	if m.mode != modeNormal || m.ops.UpdatePending() {
		t.Fatalf("the update must not open a view on its own: mode=%v", m.mode)
	}
	if b := m.ops.Batch(); b.Op != domain.OperationUpdate || len(b.Rows) != 1 ||
		b.Rows[0].Result.Error.Error() != "fetch failed: remote unavailable" {
		t.Fatalf("rows = %+v", b.Rows)
	}
}

// TestCancelDuringFetchCancelsUpdate checks that c drops the confirmed update
// even if the remaining fetches still succeed.
func TestCancelDuringFetchCancelsUpdate(t *testing.T) {
	m := newModel(t)
	r := &m.repos[0]
	r.Availability = domain.Available
	offline(m)
	m.preview = &updatePreview{targets: []domain.RepositoryID{r.ID}}
	m.startUpdate()
	if m.mode != modeOperations || !m.ops.FetchForUpdate() {
		t.Fatalf("the confirmed update must fetch first: mode=%v", m.mode)
	}
	keys(m, "R")
	if m.loadingInventory {
		t.Fatal("R must not drop an update that waits for its fetch")
	}
	keys(m, "c")
	setRow(m, 0, domain.StatusSucceeded, nil)
	m.advanceUpdate()
	if m.ops.UpdatePending() || m.ops.Batch().Op != domain.OperationFetch || m.message != "update cancelled" {
		t.Fatalf("the update must not start after c: op=%s message=%q", m.ops.Batch().Op, m.message)
	}
}

func TestMonochromeSelectionKeepsDefaultColorsReadable(t *testing.T) {
	th := newTheme(config.ThemePresets["monochrome"])
	for _, s := range []lipgloss.Style{th.err, th.key, th.branch, th.dim} {
		if fg := th.onSelected(s).GetForeground(); fg != th.selectedFg {
			t.Errorf("the default foreground is not readable on the selected background, got %v", fg)
		}
	}
}

func TestOperationsKeepGroupTitleWhenScrolled(t *testing.T) {
	m := newModel(t)
	var ids []domain.RepositoryID
	for i := 0; i < 40; i++ {
		ids = append(ids, domain.RepositoryID(fmt.Sprint(i)))
	}
	fetchBatch(t, m, ids...)
	for i := range ids {
		setRow(m, i, domain.StatusCancelled, nil)
	}
	keys(m, "b")
	for i := 0; i < 30; i++ {
		keys(m, "j")
	}
	if v := m.View(); !strings.Contains(v, "CANCELLED 40") || !strings.Contains(v, "– 30 ") {
		t.Fatalf("the title of the shown group must stay visible:\n%s", v)
	}
}

func TestColumnsFollowContent(t *testing.T) {
	// The longest name, "Engine Portal" and a long branch fit without
	// truncation; the remaining space is not given to the name column. Count
	// columns are as wide as their full titles.
	ic := iconSet("unicode")
	natural := []int{0, 1, 24, 13, 26, 7, 1, 1, 1, 1, 2, 1, 12}
	cols := columns(166, natural, ic, false)
	for i, want := range []int{3, 1, 24, 13, 26, 16, 6, 8, 9, 7, 5, 6, 12} {
		if cols[i].width != want {
			t.Errorf("%s: width %d, want %d", cols[i].title, cols[i].width, want)
		}
	}
	if !cols[colStaged].right || cols[colName].right {
		t.Error("only count columns are right-aligned")
	}
	// If space is short, count columns switch to their symbols, the table
	// still fits, and the state shrinks last.
	for _, iw := range []int{64, 80, 100} {
		cols := columns(iw, natural, ic, false)
		if w := tableWidth(cols); w > iw {
			t.Errorf("iw %d: table width %d", iw, w)
		}
		if cols[colStaged].title != "✚" || cols[colBehind].title != "↓" || cols[colAhead].width != 2 {
			t.Errorf("iw %d: count columns must use their symbols: %+v", iw, cols)
		}
		if cols[colState].width < 6 {
			t.Errorf("iw %d: state shrinks last: %+v", iw, cols)
		}
	}
	// The tree names the family in its family rows, so the column is hidden
	// and its space goes to the full count titles.
	cols = columns(120, natural, ic, true)
	if !cols[colFamily].hidden || cols[colStaged].title != "Staged" || tableWidth(cols) > 120 {
		t.Errorf("tree: %+v", cols)
	}
}

// TestNamesStartInOneColumn checks that health symbols of different widths
// do not shift the names.
func TestNamesStartInOneColumn(t *testing.T) {
	m := newModel(t)
	m.icons = iconSet("ascii")
	r := &m.repos[m.index["/r/frontend"]]
	r.Availability = domain.Available
	r.Head = domain.KnownValue(domain.HeadState{Branch: "main"})
	r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true})
	r.Operation = domain.KnownValue(domain.NoOperation)
	if s, healthy := m.health(r); !healthy || s.text != "ok" {
		t.Fatalf("frontend must be healthy: %q", s.text)
	}
	col := map[string]int{}
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		for _, name := range []string{"frontend", "dotfiles"} {
			if _, seen := col[name]; seen || !strings.HasPrefix(l, "│") {
				continue
			}
			if i := strings.Index(l, " "+name+" "); i >= 0 {
				col[name] = runewidth.StringWidth(l[:i])
			}
		}
	}
	if len(col) != 2 || col["frontend"] != col["dotfiles"] {
		t.Fatalf("names must start in one column: %v", col)
	}
}

// TestBranchSymbolOnlyInDetails checks that the titled Branch column shows
// the name alone, while the detail panel keeps the symbol.
func TestBranchSymbolOnlyInDetails(t *testing.T) {
	m := newModel(t)
	m.icons = iconSet("nerd")
	r := repo("svc", "Work", false)
	r.Availability = domain.Available
	r.Head = domain.KnownValue(domain.HeadState{Branch: "main"})
	r.Upstream = domain.KnownValue(domain.UpstreamState{Configured: true})
	if got := plain(m.rowCells(&r, time.Now())[colBranch-colHealth]); got != "main" {
		t.Errorf("branch cell = %q, want the name alone", got)
	}
	if v := strings.Join(m.detailLines(&r, 60).fit(100), "\n"); !strings.Contains(v, m.icons.branch+"main") {
		t.Errorf("the detail panel must keep the branch symbol:\n%s", v)
	}
}

// TestDetailPanelMovesToTheTable checks that on a wide terminal the
// repository panel is only as wide as its table and the detail panel takes
// all space left over, however wide the terminal is.
func TestDetailPanelMovesToTheTable(t *testing.T) {
	m := newModel(t)
	for _, width := range []int{220, 320} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		need := m.tableNeed(m.measureTable())
		if width-need <= maxDetailWidth {
			t.Fatalf("width %d: the test table must leave more than %d columns: need %d", width, maxDetailWidth, need)
		}
		found := false
		for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
			if i := strings.Index(l, "╮╭"); i >= 0 {
				found = true
				if got := runewidth.StringWidth(l[:i]) + 1; got != need || runewidth.StringWidth(l) != width {
					t.Errorf("width %d: the repository panel is %d wide, want %d:\n%s", width, got, need, l)
				}
			}
		}
		if !found {
			t.Fatalf("width %d: the panels must be side by side", width)
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
	if v := m.View(); !strings.Contains(v, "[-] Work (1)") || !strings.Contains(v, "`- x api") {
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

func TestHelpExplainsHealthColumn(t *testing.T) {
	m := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 80})
	for _, set := range []string{"unicode", "nerd", "ascii"} {
		m.icons = iconSet(set)
		keys(m, "?")
		v := ansi.Strip(m.View())
		if !strings.Contains(v, "Health column") {
			t.Fatalf("%s: the help must have a health legend:\n%s", set, v)
		}
		for _, b := range m.icons.healthLegend() {
			if !strings.Contains(v, b.Keys) || !strings.Contains(v, b.Action) {
				t.Errorf("%s: the help must explain %q (%s)", set, b.Keys, b.Action)
			}
		}
		keys(m, "esc")
	}
}

// TestBindingsDocumented keeps docs/keybindings.md in step with the help
// view: every key of a help group appears in the first column of the
// documentation section with the same title.
func TestBindingsDocumented(t *testing.T) {
	data, err := os.ReadFile("../../docs/keybindings.md")
	if err != nil {
		t.Fatal(err)
	}
	sections := map[string]map[string]bool{}
	var keys map[string]bool
	for _, line := range strings.Split(string(data), "\n") {
		if title, ok := strings.CutPrefix(line, "## "); ok {
			keys = map[string]bool{}
			sections[title] = keys
			continue
		}
		if keys == nil || !strings.HasPrefix(line, "| `") {
			continue
		}
		cell := strings.Split(line, "|")[1]
		for _, part := range strings.Split(cell, "`")[1:] {
			keys[part] = true
		}
	}
	aliases := strings.NewReplacer("S-Tab", "Shift+Tab")
	for _, g := range Bindings {
		doc, ok := sections[g.Title]
		if !ok {
			t.Errorf("docs/keybindings.md has no section %q", g.Title)
			continue
		}
		for _, b := range g.Bindings {
			for _, k := range strings.FieldsFunc(aliases.Replace(b.Keys), func(r rune) bool { return r == '/' || r == '…' }) {
				if k = strings.TrimSpace(k); !doc[k] {
					t.Errorf("section %q of docs/keybindings.md does not document key %q", g.Title, k)
				}
			}
		}
	}
}

func TestFailedInspectionAppliesPartialStateAndJobError(t *testing.T) {
	m := newModel(t)
	id := m.repos[0].ID
	ev := inspectionEvent(m, id, app.InspectionPayload{Generation: 5, Inspection: domain.Inspection{
		Stashes: domain.KnownValue(2)}})
	ev.events[0].Result.Status = domain.StatusFailed
	ev.events[0].Result.Error = errors.New("timed out after 1s")
	m.Update(ev)
	r := m.repo(id)
	if r.Inspected.Err == nil || r.Inspected.Err.Error() != "timed out after 1s" || r.Stashes.Value != 2 {
		t.Fatalf("err=%v stashes=%v", r.Inspected.Err, r.Stashes)
	}
	if m.pendingInspect() != 0 || len(m.routes) != 0 {
		t.Fatalf("finished inspection batch must be forgotten: pending=%d routes=%d", m.pendingInspect(), len(m.routes))
	}
}
