package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mmo-con/git-dashboard/internal/config"
)

func TestBrowserURL(t *testing.T) {
	tests := map[string]string{
		"git@github.com:example/api.git":          "https://github.com/example/api",
		"ssh://git@gitlab.com/group/sub/repo.git": "https://gitlab.com/group/sub/repo",
		"ssh://git@codeberg.org:22/user/repo":     "https://codeberg.org/user/repo",
		"https://user:token@example.com/x/y.git":  "https://example.com/x/y",
		"http://example.com/x/y/":                 "https://example.com/x/y",
		"git@my-host-alias:example/api.git":       "",
		"/srv/git/repo.git":                       "",
		"git@github.com:":                         "",
	}
	for in, want := range tests {
		got, ok := BrowserURL(in)
		if got != want || ok != (want != "") {
			t.Errorf("%q: got %q %v, want %q", in, got, ok, want)
		}
	}
}

func TestExpandArgs(t *testing.T) {
	got := ExpandArgs([]string{"{path}", "--dir={path}", "."}, "/repo")
	if strings.Join(got, " ") != "/repo --dir={path} ." {
		t.Fatalf("got %v", got)
	}
}

// helper writes an executable script that records its working directory and
// environment, like the controlled helper described in the design.
func helper(t *testing.T) (path, out string) {
	dir := t.TempDir()
	out = filepath.Join(dir, "out")
	path = filepath.Join(dir, "helper")
	script := "#!/bin/sh\npwd > " + out + "\necho \"GIT_DIR=${GIT_DIR:-unset}\" >> " + out + "\necho \"$@\" >> " + out + "\nexit 3\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, out
}

func TestLazygitHandoverCommand(t *testing.T) {
	exe, out := helper(t)
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", "/elsewhere/.git")
	lg := NewLazygit(config.Lazygit{Command: exe, Args: []string{"--debug"}, Enabled: true}, "")
	if !lg.Available() {
		t.Fatal(lg.Err)
	}
	cmd := lg.Command(repo)
	err = cmd.Run()
	if code := cmd.ProcessState.ExitCode(); code != 3 || err == nil {
		t.Fatalf("exit code %d, err %v", code, err)
	}
	data, _ := os.ReadFile(out)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if lines[0] != repo || lines[1] != "GIT_DIR=unset" || lines[2] != "--debug" {
		t.Fatalf("helper output: %q", lines)
	}
}

func TestLazygitUnavailable(t *testing.T) {
	lg := NewLazygit(config.Lazygit{Command: "definitely-not-installed-xyz", Enabled: true}, "/cfg/config.toml")
	if lg.Available() || !strings.Contains(lg.Err.Error(), "definitely-not-installed-xyz") ||
		!strings.Contains(lg.Err.Error(), "/cfg/config.toml") {
		t.Fatalf("err = %v", lg.Err)
	}
	if NewLazygit(config.Lazygit{Command: "sh", Enabled: false}, "").Available() {
		t.Fatal("disabled lazygit must be unavailable")
	}
	if _, err := Resolve(t.TempDir()); err == nil {
		t.Fatal("a directory is not an executable")
	}
}

func TestCustomLauncher(t *testing.T) {
	exe, out := helper(t)
	repo, _ := filepath.EvalSymlinks(t.TempDir())
	ls := NewCustom([]config.Launcher{{Name: "x", Command: []string{exe, "{path}"}}, {Name: "y", Command: []string{"nope-xyz"}}})
	if ls[0].Err != nil || ls[1].Err == nil {
		t.Fatalf("resolution: %v %v", ls[0].Err, ls[1].Err)
	}
	_ = ls[0].Command(repo).Run()
	data, _ := os.ReadFile(out)
	if !strings.HasSuffix(strings.TrimSpace(string(data)), repo) {
		t.Fatalf("output: %q", data)
	}
}
