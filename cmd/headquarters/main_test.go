package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tu "github.com/mmo-con/headquarters/internal/testutil"
)

func TestMakeReleaseTag(t *testing.T) {
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make is not installed")
	}
	makefile, err := filepath.Abs(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	tu.IsolateGit(t)
	repo := tu.InitRepo(t, t.TempDir())
	makeTag := func(tag string) (string, error) {
		cmd := exec.Command(makePath, "-f", makefile, "tag", "TAG="+tag)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	for _, invalid := range []string{"", "1.0.1", "v1.0", "v1.0.1-rc.1", "v1.0.1\nv1.0.2"} {
		if out, err := makeTag(invalid); err == nil || !strings.Contains(out, "Usage: make tag TAG=vMAJOR.MINOR.PATCH") {
			t.Errorf("make tag TAG=%q: err=%v, output=%s", invalid, err, out)
		}
	}
	if tags := tu.Git(t, repo, "tag", "--list"); tags != "" {
		t.Fatalf("invalid input created tags: %s", tags)
	}

	if out, err := makeTag("v1.0.1"); err != nil {
		t.Fatalf("make tag: %v\n%s", err, out)
	}
	ref := "refs/tags/v1.0.1"
	if got := tu.Git(t, repo, "for-each-ref", "--format=%(objecttype) %(contents:subject)", ref); got != "tag headquarters v1.0.1" {
		t.Errorf("unexpected tag type or message: %q", got)
	}
	head := tu.Git(t, repo, "rev-parse", "HEAD")
	tu.Commit(t, repo, "README", "next\n", "next commit")
	if out, err := makeTag("v1.0.1"); err == nil {
		t.Errorf("make tag unexpectedly accepted a duplicate: %s", out)
	}
	if got := tu.Git(t, repo, "rev-parse", ref+"^{commit}"); got != head {
		t.Errorf("tagged commit = %s, want %s", got, head)
	}
}

func runCLI(t *testing.T, env map[string]string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut, func(k string) string { return env[k] })
	return code, out.String(), errOut.String()
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVersionAndUsage(t *testing.T) {
	if code, out, _ := runCLI(t, nil, "--version"); code != 0 || out != "headquarters dev\n" {
		t.Fatalf("--version: code=%d out=%q", code, out)
	}
	if code, _, errOut := runCLI(t, nil, "--unknown"); code != 2 || !strings.Contains(errOut, "flag provided but not defined") {
		t.Fatalf("unknown flag: code=%d stderr=%q", code, errOut)
	}
	if code, _, errOut := runCLI(t, nil, "-h"); code != 0 || !strings.Contains(errOut, "usage: headquarters [options] [path]") {
		t.Fatalf("-h: code=%d stderr=%q", code, errOut)
	}
}

func TestConfigurationErrorsExitWithTwo(t *testing.T) {
	path := writeConfig(t, "[git]\nunknown_key = 1\n")
	code, _, errOut := runCLI(t, nil, "--config", path, "--check")
	if code != 2 || !strings.Contains(errOut, "invalid configuration") || !strings.Contains(errOut, "unknown_key") {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestUnusableGitExitsWithOne(t *testing.T) {
	path := writeConfig(t, "[git]\nexecutable = \"/nonexistent/git\"\n")
	if code, _, errOut := runCLI(t, nil, "--config", path, "--check"); code != 1 || errOut == "" {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
}

func TestCheckPrintsInventoryAndWarnings(t *testing.T) {
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	repo := tu.InitRepo(t, filepath.Join(base, "api"))
	missing := filepath.Join(base, "missing")
	path := writeConfig(t, `
[[repositories]]
path = "`+repo+`"
family = "Work"

[[repositories]]
path = "`+missing+`"

[lazygit]
command = "/nonexistent/lazygit"

[[launchers]]
name = "Broken"
command = ["/nonexistent/editor"]
`)
	code, out, errOut := runCLI(t, nil, "--config", path, "--check")
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{
		"configuration: " + path,
		"Work",
		"available",
		repo,
		"missing",
		missing,
		"warning: cannot start lazygit",
		"warning: launcher Broken",
		"2 repositories",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestCheckWithoutConfigurationFile(t *testing.T) {
	tu.IsolateGit(t)
	dir := t.TempDir()
	code, out, _ := runCLI(t, map[string]string{"XDG_CONFIG_HOME": dir}, "--check")
	want := "no configuration file at " + filepath.Join(dir, "headquarters", "config.toml")
	if code != 0 || !strings.Contains(out, want) || !strings.Contains(out, "0 repositories") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestScanPathReplacesConfiguredRepositories(t *testing.T) {
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	configured := tu.InitRepo(t, filepath.Join(base, "configured"))
	scanned := tu.InitRepo(t, filepath.Join(base, "scan", "team", "api"))
	path := writeConfig(t, `
[[repositories]]
path = "`+configured+`"
`)
	code, out, errOut := runCLI(t, nil, "--config", path, "--check", filepath.Join(base, "scan"))
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"configuration: " + path, "scan of " + filepath.Join(base, "scan"), scanned, "1 repositories"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, configured) {
		t.Errorf("configured repository must be replaced:\n%s", out)
	}

	t.Chdir(scanned)
	if code, out, _ := runCLI(t, map[string]string{"XDG_CONFIG_HOME": base}, "--check", "."); code != 0 || !strings.Contains(out, scanned) {
		t.Fatalf("relative path to a repository: code=%d out=%q", code, out)
	}
}

func TestInvalidScanPathExitsWithTwo(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{filepath.Join(dir, "missing")}, "no such file"},
		{[]string{file}, "is not a directory"},
		{[]string{dir, dir}, "at most one path"},
		{[]string{dir, "--check"}, "options must precede the path"},
	} {
		if code, _, errOut := runCLI(t, nil, tc.args...); code != 2 || !strings.Contains(errOut, tc.want) {
			t.Errorf("%v: code=%d stderr=%q", tc.args, code, errOut)
		}
	}
}

func TestInitPrintsAndWritesConfiguration(t *testing.T) {
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	src := filepath.Join(base, "src")
	api := tu.InitRepo(t, filepath.Join(src, "work", "api"))
	if err := os.Mkdir(filepath.Join(src, "locked"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(src, "locked"), 0o755) })

	code, out, errOut := runCLI(t, nil, "init", src)
	if code != 0 || !strings.HasPrefix(out, "# Generated by: headquarters init "+src+"\n") || !strings.Contains(out, "family = 'work'") {
		t.Fatalf("code=%d stderr=%q\n%s", code, errOut, out)
	}
	if os.Geteuid() != 0 && !strings.Contains(errOut, "warning: discovery:") {
		t.Errorf("warning missing on stderr: %q", errOut)
	}

	path := filepath.Join(base, "config", "config.toml")
	if code, out, errOut := runCLI(t, nil, "init", "-o", path, src); code != 0 || out != "" {
		t.Fatalf("-o: code=%d stdout=%q stderr=%q", code, out, errOut)
	}
	if code, out, _ := runCLI(t, nil, "--config", path, "--check"); code != 0 || !strings.Contains(out, "work") || !strings.Contains(out, api) {
		t.Fatalf("--check of generated file: code=%d\n%s", code, out)
	}
	if code, _, errOut := runCLI(t, nil, "init", "-o", path, src); code != 1 || !strings.Contains(errOut, "--force") {
		t.Fatalf("existing file: code=%d stderr=%q", code, errOut)
	}
	if code, _, errOut := runCLI(t, nil, "init", "-o", path, "--force", src); code != 0 {
		t.Fatalf("--force: code=%d stderr=%q", code, errOut)
	}
}

func TestInitErrors(t *testing.T) {
	tu.IsolateGit(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.toml")
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"init"}, 2, "./init"},
		{[]string{"init", filepath.Join(dir, "missing")}, 2, "no such file"},
		{[]string{"init", file}, 2, "is not a directory"},
		{[]string{"init", dir, "-o", out}, 2, "options must precede the directories"},
		{[]string{"init", "--max-depth", "0", dir}, 2, "--max-depth must be at least 1"},
		{[]string{"init", "--config", file, dir}, 2, "flag provided but not defined"},
		{[]string{"--check", "init", dir}, 2, "init must be the first argument"},
		{[]string{"init", "-o", out, dir}, 1, "no repositories found"},
	} {
		if code, _, errOut := runCLI(t, nil, tc.args...); code != tc.code || !strings.Contains(errOut, tc.want) {
			t.Errorf("%v: code=%d stderr=%q", tc.args, code, errOut)
		}
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("file written without repositories: %v", err)
	}
}

func TestDirectoryNamedInitIsStillScanned(t *testing.T) {
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	repo := tu.InitRepo(t, filepath.Join(base, "init", "repo"))
	t.Chdir(base)
	if code, out, _ := runCLI(t, map[string]string{"XDG_CONFIG_HOME": base}, "--check", "./init"); code != 0 || !strings.Contains(out, repo) {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

// statusRepos creates repositories for the status command and returns the
// configuration path and the upstream of the clone named clean:
//
//	Work/clean      clone of upstream, never fetched
//	Work/dirty      modified file
//	Personal/notes  credentials in the remote URL, control characters in the subject
//	Work/missing    missing directory
func statusRepos(t *testing.T) (config, upstream, clean string) {
	t.Helper()
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	upstream = tu.InitRepo(t, filepath.Join(base, "upstream"))
	clean = filepath.Join(base, "work", "clean")
	tu.Git(t, "", "clone", "-q", upstream, clean)
	dirty := tu.InitRepo(t, filepath.Join(base, "work", "dirty"))
	tu.WriteFile(t, filepath.Join(dirty, "README"), "changed\n")
	notes := tu.InitRepo(t, filepath.Join(base, "personal", "notes"))
	tu.Commit(t, notes, "a", "a\n", "evil \x1b]0;pwned\x07 subject")
	tu.Git(t, notes, "remote", "add", "origin", "https://user:secret@example.com/notes.git")
	config = writeConfig(t, `
[[repositories]]
path = "`+clean+`"
family = "Work"

[[repositories]]
path = "`+dirty+`"
family = "Work"

[[repositories]]
path = "`+notes+`"
family = "Personal"

[[repositories]]
path = "`+filepath.Join(base, "work", "missing")+`"
family = "Work"
`)
	return config, upstream, clean
}

type statusDoc struct {
	Schema       int              `json:"schema"`
	GeneratedAt  string           `json:"generated_at"`
	Summary      map[string]any   `json:"summary"`
	Repositories []map[string]any `json:"repositories"`
}

func statusJSON(t *testing.T, wantCode int, args ...string) statusDoc {
	t.Helper()
	code, out, errOut := runCLI(t, nil, append([]string{"status"}, args...)...)
	if code != wantCode {
		t.Fatalf("status %v: code=%d, want %d\nstderr=%s", args, code, wantCode, errOut)
	}
	var doc statusDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, out)
	}
	return doc
}

func names(doc statusDoc) string {
	var out []string
	for _, r := range doc.Repositories {
		out = append(out, r["name"].(string))
	}
	return strings.Join(out, " ")
}

func entry(t *testing.T, doc statusDoc, name string) map[string]any {
	t.Helper()
	for _, r := range doc.Repositories {
		if r["name"] == name {
			return r
		}
	}
	t.Fatalf("no repository %q in %s", name, names(doc))
	return nil
}

// field returns the value at a dotted path, such as "upstream.behind".
func field(r map[string]any, path string) any {
	var v any = r
	for _, k := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func TestStatusReportsEveryRepository(t *testing.T) {
	config, _, clean := statusRepos(t)
	doc := statusJSON(t, 0, "--config", config, "--json")
	if doc.Schema != 1 || doc.GeneratedAt == "" {
		t.Fatalf("schema=%d generated_at=%q", doc.Schema, doc.GeneratedAt)
	}
	if got := names(doc); got != "clean dirty missing notes" {
		t.Fatalf("repositories: %s", got)
	}
	if doc.Summary["total"] != 4.0 || doc.Summary["dirty"] != 1.0 || doc.Summary["unknown"] != 0.0 {
		t.Errorf("summary: %v", doc.Summary)
	}
	r := entry(t, doc, "clean")
	for path, want := range map[string]any{
		"path": clean, "family": "Work", "availability": "available", "known": true,
		"branch": "main", "head": "attached", "operation": "none",
		"counts.modified": 0.0, "counts.untracked": 0.0, "counts.stashed": 0.0,
		"upstream.name": "origin/main", "upstream.ahead": 0.0, "upstream.behind": 0.0,
		"remote.name": "origin", "last_commit.subject": "initial commit",
		"last_fetch": nil, "attention_branches": 0.0, "fetch": nil, "error": nil,
		// never fetched
		"health": "attention",
	} {
		if got := field(r, path); got != want {
			t.Errorf("clean.%s = %#v, want %#v", path, got, want)
		}
	}
	if field(r, "last_commit.hash") == nil || field(r, "last_commit.time") == nil {
		t.Errorf("last commit incomplete: %v", r["last_commit"])
	}
	d := entry(t, doc, "dirty")
	if field(d, "counts.modified") != 1.0 || d["health"] != "dirty" || field(d, "upstream.configured") != false {
		t.Errorf("dirty: %v", d)
	}
}

func TestStatusNeverReportsUnknownStateAsClean(t *testing.T) {
	config, _, _ := statusRepos(t)
	r := entry(t, statusJSON(t, 0, "--config", config), "missing")
	if r["known"] != false || r["health"] != "error" || r["availability"] != "missing" || r["error"] == nil {
		t.Errorf("missing: %v", r)
	}
	for _, k := range []string{"branch", "head", "operation", "counts", "upstream", "last_commit", "attention_branches"} {
		if v, ok := r[k]; !ok || v != nil {
			t.Errorf("missing.%s = %#v, want null", k, v)
		}
	}
}

func TestStatusFilter(t *testing.T) {
	config, _, _ := statusRepos(t)
	for filter, want := range map[string]string{
		"is:dirty family:work": "dirty",
		"family:personal":      "notes",
		"is:unavailable":       "missing",
	} {
		doc := statusJSON(t, 0, "--config", config, "--filter", filter)
		if got := names(doc); got != want {
			t.Errorf("%q: got %q, want %q", filter, got, want)
		}
		if doc.Summary["total"] != float64(len(strings.Fields(want))) {
			t.Errorf("%q: summary counts unselected repositories: %v", filter, doc.Summary)
		}
	}
	for _, filter := range []string{"is:bogus", "is:marked", "family:"} {
		code, out, errOut := runCLI(t, nil, "status", "--config", config, "--filter", filter)
		if code != 2 || out != "" || !strings.Contains(errOut, "filter") {
			t.Errorf("%q: code=%d out=%q stderr=%q", filter, code, out, errOut)
		}
	}
}

func TestStatusNDJSON(t *testing.T) {
	config, _, _ := statusRepos(t)
	code, out, errOut := runCLI(t, nil, "status", "--config", config, "--format", "ndjson")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want one line per repository:\n%s", out)
	}
	for _, line := range lines {
		var r map[string]any
		if err := json.Unmarshal([]byte(line), &r); err != nil || r["schema"] != 1.0 || r["name"] == nil {
			t.Errorf("line %q: %v %v", line, err, r)
		}
	}
	if code, _, _ := runCLI(t, nil, "status", "--config", config, "--json", "--format", "ndjson"); code != 2 {
		t.Errorf("--json with --format ndjson: code=%d", code)
	}
}

func TestStatusFailOn(t *testing.T) {
	config, _, _ := statusRepos(t)
	statusJSON(t, 3, "--config", config, "--fail-on", "dirty")
	statusJSON(t, 3, "--config", config, "--fail-on", "behind,unavailable")
	statusJSON(t, 0, "--config", config, "--fail-on", "dirty,unknown", "--filter", "family:personal")
	if code, _, errOut := runCLI(t, nil, "status", "--config", config, "--fail-on", "dirty,bogus"); code != 2 || !strings.Contains(errOut, "bogus") {
		t.Errorf("invalid state: code=%d stderr=%q", code, errOut)
	}
}

func TestStatusFetchesOnlyWhenAsked(t *testing.T) {
	config, upstream, clean := statusRepos(t)
	fetchHead := filepath.Join(clean, ".git", "FETCH_HEAD")
	tu.Commit(t, upstream, "new", "new\n", "upstream commit")

	r := entry(t, statusJSON(t, 0, "--config", config), "clean")
	if _, err := os.Stat(fetchHead); !os.IsNotExist(err) || field(r, "upstream.behind") != 0.0 {
		t.Fatalf("status without --fetch fetched: %v, %v", err, r["upstream"])
	}
	statusJSON(t, 0, "--config", config, "--fetch", "--filter", "family:personal")
	if _, err := os.Stat(fetchHead); !os.IsNotExist(err) {
		t.Fatal("--fetch fetched a repository outside the filter")
	}

	doc := statusJSON(t, 3, "--config", config, "--fetch", "--fail-on", "error")
	r = entry(t, doc, "clean")
	if field(r, "upstream.behind") != 1.0 || field(r, "fetch.status") != "succeeded" || r["last_fetch"] == nil || r["health"] != "behind" {
		t.Errorf("clean after fetch: %v", r)
	}
	n := entry(t, doc, "notes")
	if field(n, "fetch.status") != "failed" || field(n, "fetch.error") == nil || n["health"] != "error" || n["error"] == nil {
		t.Errorf("failed fetch must be reported: %v", n)
	}
}

func TestStatusSanitizesRepositoryText(t *testing.T) {
	config, _, _ := statusRepos(t)
	code, out, _ := runCLI(t, nil, "status", "--config", config, "--filter", "family:personal")
	if code != 0 || strings.Contains(out, "secret") || strings.Contains(out, `\u001b`) || strings.Contains(out, `\u0007`) {
		t.Fatalf("code=%d out=%s", code, out)
	}
	var doc statusDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	r := entry(t, doc, "notes")
	if got := field(r, "remote.url"); got != "https://***@example.com/notes.git" {
		t.Errorf("remote.url = %v", got)
	}
}
