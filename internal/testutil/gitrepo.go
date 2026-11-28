// Package testutil creates real Git repositories for integration tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// IsolateGit makes Git ignore the user's global and system configuration and
// sets a deterministic identity.
func IsolateGit(t testing.TB) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("LC_ALL", "C")
	// Besides the redirecting variables, configuration passed through the
	// environment would apply to every test repository.
	for _, v := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_NAMESPACE",
		"GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	Git(t, "", "config", "--global", "init.defaultBranch", "main")
	Git(t, "", "config", "--global", "advice.detachedHead", "false")
}

// Git runs git in dir and returns trimmed standard output. It fails the test
// on error.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	out, err := TryGit(dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(out)
}

// TryGit runs git in dir and returns combined output.
func TryGit(dir string, args ...string) (string, error) {
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// InitRepo creates a repository with one commit at path.
func InitRepo(t testing.TB, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, path, "init", "-q")
	Commit(t, path, "README", "init\n", "initial commit")
	return path
}

// Commit writes a file and commits it.
func Commit(t testing.TB, repo, file, content, message string) {
	t.Helper()
	WriteFile(t, filepath.Join(repo, file), content)
	Git(t, repo, "add", file)
	Git(t, repo, "commit", "-q", "-m", message)
}

// WriteFile writes content to path, creating parent directories.
func WriteFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Canonical resolves symbolic links in a temporary path, for example
// /var -> /private/var on macOS.
func Canonical(t testing.TB, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
