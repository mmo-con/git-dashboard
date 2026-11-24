package initconfig_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/mmo-con/headquarters/internal/config"
	"github.com/mmo-con/headquarters/internal/git"
	"github.com/mmo-con/headquarters/internal/initconfig"
	"github.com/mmo-con/headquarters/internal/inventory"
	tu "github.com/mmo-con/headquarters/internal/testutil"
)

type discovery struct {
	Root     string   `toml:"root"`
	MaxDepth int      `toml:"max_depth"`
	Family   string   `toml:"family"`
	Exclude  []string `toml:"exclude"`
}

type repository struct {
	Path   string `toml:"path"`
	Family string `toml:"family"`
}

type file struct {
	Version      int          `toml:"version"`
	Discovery    []discovery  `toml:"discovery"`
	Repositories []repository `toml:"repositories"`
}

func generate(t *testing.T, opts initconfig.Options) (initconfig.Result, file) {
	t.Helper()
	if opts.MaxDepth == 0 {
		opts.MaxDepth = 3
	}
	res, err := initconfig.Generate(context.Background(), opts, git.New("git"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var f file
	if err := toml.Unmarshal(res.Config, &f); err != nil {
		t.Fatalf("generated configuration does not parse: %v\n%s", err, res.Config)
	}
	return res, f
}

func newRoot(t *testing.T) string {
	t.Helper()
	tu.IsolateGit(t)
	return tu.Canonical(t, t.TempDir())
}

func TestRepositoriesDirectlyBelowRootYieldOneUngroupedEntry(t *testing.T) {
	root := newRoot(t)
	tu.InitRepo(t, filepath.Join(root, "a"))
	tu.InitRepo(t, filepath.Join(root, "b"))

	res, f := generate(t, initconfig.Options{Roots: []string{root}})

	want := []discovery{{Root: root, MaxDepth: 3, Family: "Ungrouped"}}
	if f.Version != 1 || !reflect.DeepEqual(f.Discovery, want) || len(f.Repositories) != 0 {
		t.Fatalf("unexpected configuration:\n%s", res.Config)
	}
	if res.Repositories != 2 {
		t.Errorf("Repositories = %d, want 2", res.Repositories)
	}
}

func TestFirstLevelDirectoryBecomesFamily(t *testing.T) {
	root := newRoot(t)
	tu.InitRepo(t, filepath.Join(root, "direct"))
	tu.InitRepo(t, filepath.Join(root, "work", "api"))
	tu.InitRepo(t, filepath.Join(root, "work", "team", "web"))
	tu.WriteFile(t, filepath.Join(root, "empty", "notes.txt"), "no repository\n")

	res, f := generate(t, initconfig.Options{Roots: []string{root}})

	want := []discovery{
		{Root: root, MaxDepth: 3, Family: "Ungrouped", Exclude: []string{"work/**"}},
		{Root: filepath.Join(root, "work"), MaxDepth: 2, Family: "work"},
	}
	if !reflect.DeepEqual(f.Discovery, want) {
		t.Fatalf("discovery = %+v\n%s", f.Discovery, res.Config)
	}
}

func TestMaxDepthBoundsScanAndGeneratedDepths(t *testing.T) {
	root := newRoot(t)
	tu.InitRepo(t, filepath.Join(root, "work", "a", "b", "deep"))
	tu.InitRepo(t, filepath.Join(root, "direct"))

	res, f := generate(t, initconfig.Options{Roots: []string{root}, MaxDepth: 4})
	want := []discovery{
		{Root: root, MaxDepth: 4, Family: "Ungrouped", Exclude: []string{"work/**"}},
		{Root: filepath.Join(root, "work"), MaxDepth: 3, Family: "work"},
	}
	if !reflect.DeepEqual(f.Discovery, want) || res.Repositories != 2 {
		t.Fatalf("depth 4: %d repositories\n%s", res.Repositories, res.Config)
	}

	res, f = generate(t, initconfig.Options{Roots: []string{root}, MaxDepth: 1})
	want = []discovery{{Root: root, MaxDepth: 1, Family: "Ungrouped"}}
	if !reflect.DeepEqual(f.Discovery, want) || res.Repositories != 1 {
		t.Fatalf("depth 1: %d repositories\n%s", res.Repositories, res.Config)
	}
}

func TestRootThatIsARepositoryBecomesExplicitEntry(t *testing.T) {
	root := newRoot(t)
	tu.InitRepo(t, root)

	res, f := generate(t, initconfig.Options{Roots: []string{root}})
	want := []repository{{Path: root, Family: "Ungrouped"}}
	if len(f.Discovery) != 0 || !reflect.DeepEqual(f.Repositories, want) || res.Repositories != 1 {
		t.Fatalf("unexpected configuration:\n%s", res.Config)
	}
}

// resolve returns the sorted repository paths and families of an inventory
// together with all configuration and inventory warnings.
func resolve(t *testing.T, cfg *config.Config) (map[string]string, []string) {
	t.Helper()
	res := inventory.Build(context.Background(), cfg, git.New("git"))
	repos := map[string]string{}
	for _, r := range res.Repositories {
		repos[r.Path] = string(r.Family)
	}
	return repos, append(append([]string{}, cfg.Warnings...), res.Warnings...)
}

func load(t *testing.T, data []byte) *config.Config {
	t.Helper()
	cfg, err := config.Parse(data, t.TempDir(), os.Getenv)
	if err != nil {
		t.Fatalf("generated configuration is invalid: %v\n%s", err, data)
	}
	return cfg
}

func scan(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	want := map[string]string{}
	for _, root := range roots {
		cfg := load(t, nil)
		cfg.ScanOnly(root)
		repos, _ := resolve(t, cfg)
		for p := range repos {
			want[p] = "Ungrouped"
		}
	}
	return want
}

func sortedKeys(m map[string]string) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestGeneratedConfigurationResolvesTheScannedRepositories(t *testing.T) {
	root := newRoot(t)
	tu.InitRepo(t, filepath.Join(root, "direct"))
	tu.InitRepo(t, filepath.Join(root, "work", "api"))
	tu.InitRepo(t, filepath.Join(root, "work", "team", "web"))
	tu.InitRepo(t, filepath.Join(root, "private", "dotfiles"))
	tu.InitRepo(t, filepath.Join(root, "other", "x", "y", "too-deep"))

	res, _ := generate(t, initconfig.Options{Roots: []string{root}})
	got, warnings := resolve(t, load(t, res.Config))

	if want := sortedKeys(scan(t, root)); !reflect.DeepEqual(sortedKeys(got), want) {
		t.Fatalf("repositories = %v, want %v\n%s", sortedKeys(got), want, res.Config)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings: %v", warnings)
	}
	wantFamilies := map[string]string{
		filepath.Join(root, "direct"):              "Ungrouped",
		filepath.Join(root, "work", "api"):         "work",
		filepath.Join(root, "work", "team", "web"): "work",
		filepath.Join(root, "private", "dotfiles"): "private",
	}
	if !reflect.DeepEqual(got, wantFamilies) {
		t.Errorf("families = %v", got)
	}
}

func TestLaterCloneIntoNewDirectoryIsFoundAsUngrouped(t *testing.T) {
	root := newRoot(t)
	tu.InitRepo(t, filepath.Join(root, "work", "api"))
	res, _ := generate(t, initconfig.Options{Roots: []string{root}})

	later := tu.InitRepo(t, filepath.Join(root, "new", "team", "service"))
	got, _ := resolve(t, load(t, res.Config))
	if got[later] != "Ungrouped" {
		t.Fatalf("later clone: %v\n%s", got, res.Config)
	}
}

func TestSeveralRootsAndOverlappingRoots(t *testing.T) {
	base := newRoot(t)
	src := filepath.Join(base, "src")
	work := filepath.Join(base, "work")
	tu.InitRepo(t, filepath.Join(src, "a"))
	tu.InitRepo(t, filepath.Join(src, "team", "b"))
	tu.InitRepo(t, filepath.Join(work, "c"))
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(src, alias); err != nil {
		t.Fatal(err)
	}

	roots := []string{src, work, filepath.Join(src, "team"), base, alias}
	res, f := generate(t, initconfig.Options{Roots: roots})

	var got []string
	for _, d := range f.Discovery {
		got = append(got, d.Root)
	}
	want := []string{src, filepath.Join(src, "team"), work}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("roots = %v, want %v\n%s", got, want, res.Config)
	}
	if res.Repositories != 3 || len(res.Warnings) != 3 {
		t.Fatalf("repositories = %d, warnings = %q", res.Repositories, res.Warnings)
	}
	repos, warnings := resolve(t, load(t, res.Config))
	if !reflect.DeepEqual(sortedKeys(repos), sortedKeys(scan(t, src, work))) || len(warnings) != 0 {
		t.Fatalf("repositories = %v, warnings = %v", sortedKeys(repos), warnings)
	}
}

var commentedEntry = regexp.MustCompile(`(?m)^# (\[\[repositories\]\]|path = |family = )`)

func TestNestedRepositoriesAreCommentedOutAndSubmodulesSkipped(t *testing.T) {
	root := newRoot(t)
	api := tu.InitRepo(t, filepath.Join(root, "work", "api"))
	tu.InitRepo(t, filepath.Join(api, "plugin"))
	tu.InitRepo(t, filepath.Join(api, "too", "deep"))
	direct := tu.InitRepo(t, filepath.Join(root, "direct"))
	tu.InitRepo(t, filepath.Join(direct, "vendor", "lib"))
	lib := tu.InitRepo(t, filepath.Join(t.TempDir(), "lib"))
	tu.Git(t, direct, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "sub")

	res, f := generate(t, initconfig.Options{Roots: []string{root}})
	if len(f.Repositories) != 0 || res.Repositories != 2 {
		t.Fatalf("nested repositories must not be active:\n%s", res.Config)
	}

	var enabled file
	if err := toml.Unmarshal(commentedEntry.ReplaceAll(res.Config, []byte("$1")), &enabled); err != nil {
		t.Fatalf("commented entries do not parse when enabled: %v\n%s", err, res.Config)
	}
	want := []repository{
		{Path: filepath.Join(direct, "vendor", "lib"), Family: "Ungrouped"},
		{Path: filepath.Join(api, "plugin"), Family: "work"},
	}
	if !reflect.DeepEqual(enabled.Repositories, want) {
		t.Fatalf("nested = %+v\n%s", enabled.Repositories, res.Config)
	}
}

func TestSpecialNames(t *testing.T) {
	base := newRoot(t)
	root := filepath.Join(base, "src")
	glob := tu.InitRepo(t, filepath.Join(root, "a[1]{x}", "repo"))
	dollar := tu.InitRepo(t, filepath.Join(root, "$HOME", "repo"))
	invalid := tu.InitRepo(t, filepath.Join(root, "bad\xff", "repo"))
	direct := tu.InitRepo(t, filepath.Join(root, "direct"))
	tu.InitRepo(t, filepath.Join(direct, "n$x"))
	tu.InitRepo(t, filepath.Join(base, "r$", "repo"))

	res, f := generate(t, initconfig.Options{Roots: []string{root, filepath.Join(base, "r$")}})

	want := []discovery{
		{Root: root, MaxDepth: 3, Family: "Ungrouped", Exclude: []string{`a\[1\]\{x\}/**`}},
		{Root: filepath.Join(root, "a[1]{x}"), MaxDepth: 2, Family: "a[1]{x}"},
	}
	if !reflect.DeepEqual(f.Discovery, want) {
		t.Fatalf("discovery = %+v\n%s", f.Discovery, res.Config)
	}
	if len(res.Warnings) != 4 || commentedEntry.Match(res.Config) {
		t.Fatalf("warnings = %q\n%s", res.Warnings, res.Config)
	}
	got, warnings := resolve(t, load(t, res.Config))
	wantRepos := map[string]string{glob: "a[1]{x}", dollar: "Ungrouped", invalid: "Ungrouped", direct: "Ungrouped"}
	if !reflect.DeepEqual(got, wantRepos) || len(warnings) != 0 {
		t.Fatalf("repositories = %v, warnings = %v", got, warnings)
	}
}

func TestOutputIsDeterministicAndUsesHome(t *testing.T) {
	tu.IsolateGit(t)
	home := tu.Canonical(t, os.Getenv("HOME"))
	t.Setenv("HOME", home)
	root := filepath.Join(home, "src")
	for _, name := range []string{"b", "a", "z/two", "y/one", "x/three", "a2/four"} {
		tu.InitRepo(t, filepath.Join(root, name))
	}
	tu.InitRepo(t, filepath.Join(root, "b", "inner"))
	tu.InitRepo(t, filepath.Join(root, "a", "inner"))

	opts := initconfig.Options{Roots: []string{root}, Home: home, Command: "headquarters init ~/src"}
	first, f := generate(t, opts)
	for range 3 {
		if again, _ := generate(t, opts); !bytes.Equal(again.Config, first.Config) {
			t.Fatalf("output differs:\n%s\n---\n%s", first.Config, again.Config)
		}
	}
	if !strings.HasPrefix(string(first.Config), "# Generated by: headquarters init ~/src\n") {
		t.Errorf("missing header:\n%s", first.Config)
	}
	var roots []string
	for _, d := range f.Discovery {
		roots = append(roots, d.Root)
	}
	want := []string{"~/src", "~/src/a2", "~/src/x", "~/src/y", "~/src/z"}
	if !reflect.DeepEqual(roots, want) || !strings.Contains(string(first.Config), "# path = '~/src/a/inner'") {
		t.Fatalf("roots = %v\n%s", roots, first.Config)
	}
	if got, warnings := resolve(t, load(t, first.Config)); len(got) != 6 || len(warnings) != 0 {
		t.Fatalf("repositories = %v, warnings = %v", got, warnings)
	}
}

func TestUnreadableDirectoryIsReportedInTheFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read every directory")
	}
	root := newRoot(t)
	tu.InitRepo(t, filepath.Join(root, "ok"))
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	res, _ := generate(t, initconfig.Options{Roots: []string{root}})
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "permission denied") {
		t.Fatalf("warnings = %q", res.Warnings)
	}
	if !strings.Contains(string(res.Config), "\n# warning: "+res.Warnings[0]+"\n") {
		t.Fatalf("warning missing in file:\n%s", res.Config)
	}
}

func TestNoRepositoriesIsAnError(t *testing.T) {
	root := newRoot(t)
	tu.WriteFile(t, filepath.Join(root, "notes", "todo.txt"), "nothing\n")
	res, err := initconfig.Generate(context.Background(), initconfig.Options{Roots: []string{root}, MaxDepth: 3}, git.New("git"))
	if !errors.Is(err, initconfig.ErrNoRepositories) || res.Config != nil {
		t.Fatalf("err = %v, config = %q", err, res.Config)
	}
}
