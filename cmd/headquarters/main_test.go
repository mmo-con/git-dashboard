package main

import (
	"bytes"
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
