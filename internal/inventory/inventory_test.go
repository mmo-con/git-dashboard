package inventory_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mmo-con/headquarters/internal/config"
	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/git"
	"github.com/mmo-con/headquarters/internal/inventory"
	tu "github.com/mmo-con/headquarters/internal/testutil"
)

// tree builds a mixed layout: repositories at different depths, ordinary
// directories, excluded and pruned trees, and a repository deeper than
// max_depth.
func tree(t *testing.T) string {
	base := tu.Canonical(t, t.TempDir())
	for _, p := range []string{
		"dotfiles", "company/api", "company/frontend", "experiments/parser",
		"experiments/compiler", "archive/legacy-service", "archive/important-tool",
		"company/deep/a/b/too-deep", "company/node_modules/dep",
	} {
		tu.InitRepo(t, filepath.Join(base, p))
	}
	for _, d := range []string{"notes", "company/documentation"} {
		if err := os.MkdirAll(filepath.Join(base, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A repository nested inside another one must never be discovered.
	tu.InitRepo(t, filepath.Join(base, "company/api/vendor/nested"))
	return base
}

func build(t *testing.T, cfg *config.Config) inventory.Result {
	t.Helper()
	return inventory.Build(context.Background(), cfg, git.New("git"))
}

func paths(base string, res inventory.Result) map[string]domain.Repository {
	m := map[string]domain.Repository{}
	for _, r := range res.Repositories {
		rel, _ := filepath.Rel(base, r.Path)
		m[rel] = r
	}
	return m
}

func keys(m map[string]domain.Repository) []string {
	var k []string
	for p := range m {
		k = append(k, p)
	}
	sort.Strings(k)
	return k
}

func TestDiscoveryAndPrecedence(t *testing.T) {
	tu.IsolateGit(t)
	base := tree(t)
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{Path: filepath.Join(base, "dotfiles"), Family: "Personal", Enabled: true},
		{Path: filepath.Join(base, "archive/important-tool"), Family: "Tools", Enabled: true},
		{Path: filepath.Join(base, "experiments/parser"), Enabled: false},
		{Path: filepath.Join(base, "company/api"), Family: "Override", Enabled: true, Untracked: "no",
			PinnedBranches: []string{"develop"}},
		{Path: filepath.Join(base, "missing"), Enabled: true},
	}
	cfg.Discovery = []config.Discovery{
		{Root: base, MaxDepth: 3, Family: "All", Include: []string{"**"},
			Exclude: []string{"archive/**", "**/node_modules/**"}},
		{Root: filepath.Join(base, "company"), MaxDepth: 2, Family: "Work", Include: []string{"**"},
			Exclude: []string{"node_modules/**"}},
	}
	res := build(t, cfg)
	got := paths(base, res)
	want := []string{"archive/important-tool", "company/api", "company/frontend", "dotfiles", "experiments/compiler", "missing"}
	if strings.Join(keys(got), ",") != strings.Join(want, ",") {
		t.Fatalf("inventory = %v, want %v", keys(got), want)
	}
	if r := got["company/api"]; r.Family != "Override" || r.Source != domain.InventorySourceExplicit || r.Untracked != domain.UntrackedNo {
		t.Errorf("explicit fields must override discovery: %+v", r)
	}
	if p := got["company/api"].BranchPolicy; !p.Enabled || !p.ReportMissing || strings.Join(p.Pinned, ",") != "develop" {
		t.Errorf("per-repository pinned branches: %+v", p)
	}
	if p := got["company/frontend"].BranchPolicy; p.ReportMissing || strings.Join(p.Pinned, ",") != "main,master" {
		t.Errorf("global pinned branches: %+v", p)
	}
	if r := got["company/frontend"]; r.Family != "All" || r.Source != domain.InventorySourceDiscovered || r.DiscoveryRoot == "" {
		t.Errorf("first discovery root supplies defaults: %+v", r)
	}
	if r := got["missing"]; r.Availability != domain.Missing {
		t.Errorf("missing explicit entry: %+v", r)
	}
	if r := got["dotfiles"]; r.StoreID == "" || r.Availability != domain.Available {
		t.Errorf("dotfiles: %+v", r)
	}
	foundWarning := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "company/frontend") && strings.Contains(w, "discovery roots") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("expected a warning about overlapping discovery roots: %v", res.Warnings)
	}
}

func TestDiscoveryDepthAndIncludes(t *testing.T) {
	tu.IsolateGit(t)
	base := tree(t)
	cfg := config.Default()
	cfg.Discovery = []config.Discovery{{Root: filepath.Join(base, "company"), MaxDepth: 3,
		Include: []string{"deep/**", "api"}, Exclude: []string{"**/node_modules/**"}}}
	got := keys(paths(base, build(t, cfg)))
	if strings.Join(got, ",") != "company/api" {
		t.Fatalf("max_depth 3 must not reach depth 4: %v", got)
	}
	cfg.Discovery[0].MaxDepth = 4
	got = keys(paths(base, build(t, cfg)))
	if strings.Join(got, ",") != "company/api,company/deep/a/b/too-deep" {
		t.Fatalf("got %v", got)
	}
	if r := paths(base, build(t, cfg))["company/api"]; r.Family != domain.UngroupedFamily {
		t.Errorf("family without configuration must be Ungrouped: %q", r.Family)
	}
}

func TestExcludedDirectoriesAreNotTraversed(t *testing.T) {
	tu.IsolateGit(t)
	if os.Getuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}
	base := tu.Canonical(t, t.TempDir())
	tu.InitRepo(t, filepath.Join(base, "keep"))
	locked := filepath.Join(base, "node_modules")
	if err := os.MkdirAll(filepath.Join(locked, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	candidates, warnings := inventory.Discover(context.Background(), config.Discovery{
		Root: base, MaxDepth: 3, Include: []string{"**"}, Exclude: []string{"**/node_modules/**"}})
	if len(warnings) != 0 {
		t.Fatalf("excluded directory was read: %v", warnings)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %v", candidates)
	}
}

func TestSymlinksAndLoops(t *testing.T) {
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	tu.InitRepo(t, filepath.Join(base, "real/repo"))
	if err := os.Symlink(filepath.Join(base, "real"), filepath.Join(base, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, filepath.Join(base, "real/loop")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Discovery = []config.Discovery{{Root: base, MaxDepth: 6, Include: []string{"**"}}}
	if got := keys(paths(base, build(t, cfg))); strings.Join(got, ",") != "real/repo" {
		t.Fatalf("without follow_symlinks: %v", got)
	}
	cfg.Discovery[0].FollowSymlinks = true
	res := build(t, cfg)
	if len(res.Repositories) != 1 {
		t.Fatalf("with follow_symlinks the repository must appear once: %+v", keys(paths(base, res)))
	}
}

func TestExplicitSubdirectoryAndDuplicates(t *testing.T) {
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	repo := tu.InitRepo(t, filepath.Join(base, "repo"))
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repo, filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{Path: filepath.Join(repo, "sub"), Enabled: true},
		{Path: repo, Enabled: true},
		{Path: filepath.Join(base, "link"), Enabled: true},
	}
	res := build(t, cfg)
	if len(res.Repositories) != 2 {
		t.Fatalf("symlinked duplicate must be merged: %+v", res.Repositories)
	}
	if res.Repositories[0].Availability != domain.NotWorktreeRoot {
		t.Errorf("subdirectory: %v", res.Repositories[0].Availability)
	}
	if len(res.Warnings) != 1 {
		t.Errorf("warnings: %v", res.Warnings)
	}
}

func TestCaseAliasesAreOneEntry(t *testing.T) {
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	repo := tu.InitRepo(t, filepath.Join(base, "Repo"))
	alias := filepath.Join(base, "repo")
	if _, err := os.Stat(alias); err != nil {
		t.Skip("the file system is case-sensitive")
	}
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{Path: alias, Family: "Work", Enabled: true},
		{Path: repo, Enabled: true},
	}
	cfg.Discovery = []config.Discovery{{Root: base, MaxDepth: 1, Include: []string{"**"}}}
	res := build(t, cfg)
	if len(res.Repositories) != 1 {
		t.Fatalf("case aliases must be merged: %v", keys(paths(base, res)))
	}
	if r := res.Repositories[0]; r.Availability != domain.Available || r.Source != domain.InventorySourceExplicit || r.Family != "Work" {
		t.Errorf("the first explicit entry must win: %+v", r)
	}
}

func TestUntrustedOwner(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("changing the owner of a repository requires root")
	}
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	explicit := tu.InitRepo(t, filepath.Join(base, "explicit"))
	discovered := tu.InitRepo(t, filepath.Join(base, "found", "discovered"))
	for _, repo := range []string{explicit, discovered} {
		err := filepath.WalkDir(repo, func(p string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			return os.Lchown(p, 65534, 65534)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{Path: explicit, Enabled: true}}
	cfg.Discovery = []config.Discovery{{Root: filepath.Join(base, "found"), MaxDepth: 1, Include: []string{"**"}}}
	got := paths(base, build(t, cfg))
	for _, rel := range []string{"explicit", "found/discovered"} {
		r, ok := got[rel]
		if !ok {
			t.Fatalf("%s is missing from the inventory: %v", rel, keys(got))
		}
		if r.Availability != domain.UntrustedOwner || r.EntryError == nil || !strings.Contains(r.EntryError.Error(), "safe.directory") {
			t.Errorf("%s: %v %v", rel, r.Availability, r.EntryError)
		}
	}
}

func TestLinkedWorktreesAreSeparateEntriesOfOneStore(t *testing.T) {
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	main := tu.InitRepo(t, filepath.Join(base, "main"))
	tu.Git(t, main, "worktree", "add", "-q", filepath.Join(base, "feature"))
	cfg := config.Default()
	cfg.Discovery = []config.Discovery{{Root: base, MaxDepth: 1, Include: []string{"**"}}}
	res := build(t, cfg)
	if len(res.Repositories) != 2 || res.Repositories[0].StoreID != res.Repositories[1].StoreID {
		t.Fatalf("%+v", res.Repositories)
	}
}

func TestNamesAreDisambiguated(t *testing.T) {
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	for _, p := range []string{"hsm1/src", "hsm2/src", "a/x/lib", "b/x/lib", "api", "other/api"} {
		tu.InitRepo(t, filepath.Join(base, p))
	}
	cfg := config.Default()
	cfg.Repositories = []config.Repository{
		{Path: filepath.Join(base, "api"), Name: "api", Enabled: true},
	}
	cfg.Discovery = []config.Discovery{{Root: base, MaxDepth: 3, Include: []string{"**"}}}
	got := map[string]string{}
	for rel, r := range paths(base, build(t, cfg)) {
		got[rel] = r.Name
	}
	want := map[string]string{
		"hsm1/src": "hsm1/src", "hsm2/src": "hsm2/src",
		"a/x/lib": "a/x/lib", "b/x/lib": "b/x/lib",
		"api": "api", "other/api": "other/api",
	}
	for rel, name := range want {
		if got[rel] != name {
			t.Errorf("%s: name = %q, want %q", rel, got[rel], name)
		}
	}
}

func TestWorkspace(t *testing.T) {
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	for _, p := range []string{"hsm1/src", "hsm2/src", "plain"} {
		tu.InitRepo(t, filepath.Join(base, p))
	}
	cfg := config.Default()
	cfg.Repositories = []config.Repository{{Path: filepath.Join(base, "hsm2/src"), Workspace: "/elsewhere", Enabled: true}}
	cfg.Discovery = []config.Discovery{
		{Root: filepath.Join(base, "hsm1"), MaxDepth: 1, Include: []string{"src"}, Workspace: ".."},
		{Root: filepath.Join(base, "hsm2"), MaxDepth: 1, Include: []string{"src"}, Workspace: ".."},
		{Root: filepath.Join(base, "plain"), MaxDepth: 1, Include: []string{"**"}},
	}
	got := paths(base, build(t, cfg))
	for rel, want := range map[string]string{
		"hsm1/src": filepath.Join(base, "hsm1"), "hsm2/src": "/elsewhere", "plain": filepath.Join(base, "plain"),
	} {
		r := got[rel]
		if dir := r.WorkspaceDir(); dir != want {
			t.Errorf("%s: workspace %q, want %q", rel, dir, want)
		}
	}
}

// TestFailedValidationIsListed checks that a discovered candidate stays in the
// inventory when Git did not finish its validation.
func TestFailedValidationIsListed(t *testing.T) {
	base := tu.Canonical(t, t.TempDir())
	for _, d := range []string{"crashed", "broken"} {
		if err := os.MkdirAll(filepath.Join(base, d, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	exe := filepath.Join(t.TempDir(), "git")
	script := "#!/bin/sh\ncase \"$2\" in *crashed) kill -KILL $$ ;; esac\nexit 128\n"
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Discovery = []config.Discovery{{Root: base, MaxDepth: 1, Include: []string{"**"}}}
	got := paths(base, inventory.Build(context.Background(), cfg, git.New(exe)))
	if k := keys(got); len(k) != 1 || k[0] != "crashed" {
		t.Fatalf("inventory = %v, want only crashed", k)
	}
	if r := got["crashed"]; r.Availability != domain.ValidationFailed || r.EntryError == nil {
		t.Errorf("crashed: %v %v", r.Availability, r.EntryError)
	}
}
