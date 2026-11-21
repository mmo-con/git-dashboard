package git

import (
	"os"
	"strings"
)

// redirectingVars override -C and would redirect every command to the same
// repository if they were inherited.
var redirectingVars = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_INDEX_FILE",
	"GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY",
	"GIT_NAMESPACE",
}

// SanitizedEnvironment returns the process environment without
// repository-redirecting GIT_* variables. It is used for every child process,
// including interactive ones.
func SanitizedEnvironment() []string {
	return sanitize(os.Environ())
}

func sanitize(env []string) []string {
	out := make([]string, 0, len(env))
outer:
	for _, kv := range env {
		for _, name := range redirectingVars {
			if strings.HasPrefix(kv, name+"=") {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

// backgroundEnvironment returns the environment for non-interactive Git
// processes that run while the TUI owns the terminal.
func backgroundEnvironment(readOnly bool) []string {
	env := withOverrides(SanitizedEnvironment(), map[string]string{
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_PAGER":           "cat",
		"PAGER":               "cat",
		"GIT_EDITOR":          "true",
		"GIT_SEQUENCE_EDITOR": "true",
	})
	if readOnly {
		env = withOverrides(env, map[string]string{"GIT_OPTIONAL_LOCKS": "0"})
	}
	return env
}

func withOverrides(env []string, overrides map[string]string) []string {
	out := make([]string, 0, len(env)+len(overrides))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if _, ok := overrides[name]; ok {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}
