package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tu "github.com/mmo-con/git-dashboard/internal/testutil"
)

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
	if code, out, _ := runCLI(t, nil, "--version"); code != 0 || out != "git-dashboard dev\n" {
		t.Fatalf("--version: code=%d out=%q", code, out)
	}
	if code, _, errOut := runCLI(t, nil, "--unknown"); code != 2 || !strings.Contains(errOut, "flag provided but not defined") {
		t.Fatalf("unknown flag: code=%d stderr=%q", code, errOut)
	}
	if code, _, _ := runCLI(t, nil, "-h"); code != 0 {
		t.Fatalf("-h must succeed: code=%d", code)
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
	want := "no configuration file at " + filepath.Join(dir, "git-dashboard", "config.toml")
	if code != 0 || !strings.Contains(out, want) || !strings.Contains(out, "0 repositories") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}
