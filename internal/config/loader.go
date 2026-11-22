package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// EnvConfig names the environment variable that selects the configuration file.
const EnvConfig = "GIT_DASHBOARD_CONFIG"

// Locate returns the configuration path: the explicit flag value, then
// GIT_DASHBOARD_CONFIG, then the XDG configuration directory.
func Locate(flagValue string, getenv func(string) string) (string, error) {
	if flagValue != "" {
		return expandPath(flagValue, "", getenv)
	}
	if v := getenv(EnvConfig); v != "" {
		return expandPath(v, "", getenv)
	}
	base := getenv("XDG_CONFIG_HOME")
	if base == "" {
		home := getenv("HOME")
		if home == "" {
			return "", errors.New("cannot determine the configuration directory: HOME is not set")
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "git-dashboard", "config.toml"), nil
}

// Load reads the configuration at path. A missing file yields the default
// configuration with File left empty and no error.
func Load(path string, getenv func(string) string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading configuration: %w", err)
	}
	cfg, err := Parse(data, filepath.Dir(path), getenv)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.File = path
	return cfg, nil
}

// Parse decodes and validates configuration data. Relative paths are resolved
// against baseDir.
func Parse(data []byte, baseDir string, getenv func(string) string) (*Config, error) {
	var f file
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return nil, fmt.Errorf("unknown configuration field:\n%s", strict.String())
		}
		var de *toml.DecodeError
		if errors.As(err, &de) {
			row, col := de.Position()
			return nil, fmt.Errorf("line %d, column %d: %s", row, col, de.Error())
		}
		return nil, err
	}
	return build(&f, baseDir, getenv)
}

func build(f *file, baseDir string, getenv func(string) string) (*Config, error) {
	cfg := Default()
	var errs []error
	addErr := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if f.Version == 0 {
		cfg.Warnings = append(cfg.Warnings, "configuration has no version; assuming version 1")
	} else if f.Version != CurrentVersion {
		addErr("unsupported configuration version %d (supported: %d)", f.Version, CurrentVersion)
	}

	g := f.Git
	if g.Executable != "" {
		p, err := expandCommand(g.Executable, baseDir, getenv)
		if err != nil {
			addErr("[git].executable: %v", err)
		}
		cfg.Git.Executable = p
	}
	if g.InspectionParallelism != nil {
		if *g.InspectionParallelism < 1 {
			addErr("[git].inspection_parallelism must be at least 1")
		}
		cfg.Git.InspectionParallelism = *g.InspectionParallelism
	}
	if g.FetchParallelism != nil {
		if *g.FetchParallelism < 1 {
			addErr("[git].fetch_parallelism must be at least 1")
		}
		cfg.Git.FetchParallelism = *g.FetchParallelism
	}
	if g.InspectionTimeout != nil {
		cfg.Git.InspectionTimeout = g.InspectionTimeout.Duration
	}
	if g.FetchTimeout != nil {
		cfg.Git.FetchTimeout = g.FetchTimeout.Duration
	}
	if g.FetchPrune != nil {
		cfg.Git.FetchPrune = *g.FetchPrune
	}
	if g.Untracked != "" {
		if !validUntracked(g.Untracked) {
			addErr("[git].untracked must be \"normal\" or \"no\", not %q", g.Untracked)
		}
		cfg.Git.Untracked = g.Untracked
	}

	b := f.Branches
	if b.Enabled != nil {
		cfg.Branches.Enabled = *b.Enabled
	}
	if b.Pinned != nil {
		cfg.Branches.Pinned = *b.Pinned
		if err := validateBranchNames(*b.Pinned); err != nil {
			addErr("[branches].pinned: %v", err)
		}
	}
	for _, p := range b.Ignore {
		if err := validatePattern(p); err != nil {
			addErr("[branches].ignore: %v", err)
		}
	}
	cfg.Branches.Ignore = b.Ignore
	if b.MaxAge != nil {
		cfg.Branches.MaxAge = b.MaxAge.Duration
	}

	if f.Lazygit.Command != "" {
		p, err := expandCommand(f.Lazygit.Command, baseDir, getenv)
		if err != nil {
			addErr("[lazygit].command: %v", err)
		}
		cfg.Lazygit.Command = p
	}
	cfg.Lazygit.Args = f.Lazygit.Args
	if f.Lazygit.Enabled != nil {
		cfg.Lazygit.Enabled = *f.Lazygit.Enabled
	}

	for i, r := range f.Repositories {
		if r.Path == "" {
			addErr("repositories[%d]: path is required", i)
			continue
		}
		p, err := expandPath(r.Path, baseDir, getenv)
		if err != nil {
			addErr("repositories[%d].path: %v", i, err)
			continue
		}
		if r.Untracked != "" && !validUntracked(r.Untracked) {
			addErr("repositories[%d].untracked must be \"normal\" or \"no\", not %q", i, r.Untracked)
		}
		if r.RemoteName != "" && r.Remote == "" {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("repositories[%d]: remote_name has no effect without remote", i))
		}
		var pinned []string
		if r.PinnedBranches != nil {
			pinned = append([]string{}, *r.PinnedBranches...)
			if err := validateBranchNames(pinned); err != nil {
				addErr("repositories[%d].pinned_branches: %v", i, err)
			}
		}
		ws, err := expandWorkspace(r.Workspace, getenv)
		if err != nil {
			addErr("repositories[%d].workspace: %v", i, err)
		}
		enabled := true
		if r.Enabled != nil {
			enabled = *r.Enabled
		}
		cfg.Repositories = append(cfg.Repositories, Repository{
			Path: p, Name: strings.TrimSpace(r.Name), Family: r.Family, Workspace: ws, Enabled: enabled, Remote: r.Remote,
			RemoteName: r.RemoteName, Untracked: r.Untracked, PinnedBranches: pinned,
		})
	}

	for i, d := range f.Discovery {
		if d.Root == "" {
			addErr("discovery[%d]: root is required", i)
			continue
		}
		root, err := expandPath(d.Root, baseDir, getenv)
		if err != nil {
			addErr("discovery[%d].root: %v", i, err)
			continue
		}
		depth := DefaultMaxDepth
		if d.MaxDepth != nil {
			depth = *d.MaxDepth
			if depth < 1 {
				addErr("discovery[%d].max_depth must be at least 1", i)
			}
		}
		include := d.Include
		if len(include) == 0 {
			include = []string{"**"}
		}
		for _, p := range append(append([]string{}, include...), d.Exclude...) {
			if err := validatePattern(p); err != nil {
				addErr("discovery[%d]: %v", i, err)
			}
		}
		ws, err := expandWorkspace(d.Workspace, getenv)
		if err != nil {
			addErr("discovery[%d].workspace: %v", i, err)
		}
		cfg.Discovery = append(cfg.Discovery, Discovery{
			Root: root, MaxDepth: depth, Family: d.Family, Workspace: ws, Include: include,
			Exclude: d.Exclude, FollowSymlinks: d.FollowSymlinks,
		})
	}

	if f.UI.Icons != "" {
		switch f.UI.Icons {
		case "unicode", "nerd", "ascii":
			cfg.UI.Icons = f.UI.Icons
		default:
			addErr("[ui].icons must be \"unicode\", \"nerd\", or \"ascii\", not %q", f.UI.Icons)
		}
	}
	if f.UI.View != "" {
		switch f.UI.View {
		case "list", "tree":
			cfg.UI.View = f.UI.View
		default:
			addErr("[ui].view must be \"list\" or \"tree\", not %q", f.UI.View)
		}
	}
	t := f.UI.Theme
	for _, c := range []struct {
		key  string
		val  string
		dest *string
	}{
		{"active_border", t.ActiveBorder, &cfg.UI.Theme.ActiveBorder},
		{"inactive_border", t.InactiveBorder, &cfg.UI.Theme.InactiveBorder},
		{"title", t.Title, &cfg.UI.Theme.Title},
		{"selected_background", t.SelectedBackground, &cfg.UI.Theme.SelectedBackground},
		{"selected_foreground", t.SelectedForeground, &cfg.UI.Theme.SelectedForeground},
		{"selected_accent", t.SelectedAccent, &cfg.UI.Theme.SelectedAccent},
		{"key", t.Key, &cfg.UI.Theme.Key},
		{"branch", t.Branch, &cfg.UI.Theme.Branch},
		{"clean", t.Clean, &cfg.UI.Theme.Clean},
		{"dirty", t.Dirty, &cfg.UI.Theme.Dirty},
		{"ahead", t.Ahead, &cfg.UI.Theme.Ahead},
		{"behind", t.Behind, &cfg.UI.Theme.Behind},
		{"success", t.Success, &cfg.UI.Theme.Success},
		{"error", t.Error, &cfg.UI.Theme.Error},
		{"warning", t.Warning, &cfg.UI.Theme.Warning},
		{"dim", t.Dim, &cfg.UI.Theme.Dim},
	} {
		if c.val == "" {
			continue
		}
		if err := validateColor(c.val); err != nil {
			addErr("[ui.theme].%s: %v", c.key, err)
			continue
		}
		*c.dest = c.val
	}

	for i, l := range f.Launchers {
		if l.Name == "" {
			addErr("launchers[%d]: name is required", i)
		}
		if len(l.Command) == 0 || l.Command[0] == "" {
			addErr("launchers[%d]: command must be a non-empty argument array", i)
			continue
		}
		exe, err := expandCommand(l.Command[0], baseDir, getenv)
		if err != nil {
			addErr("launchers[%d].command: %v", i, err)
		}
		cmd := append([]string{exe}, l.Command[1:]...)
		for _, a := range cmd[1:] {
			for _, ph := range []string{"{path}", "{workspace}"} {
				if strings.Contains(a, ph) && a != ph {
					addErr("launchers[%d]: %s must be a complete argument, not part of %q", i, ph, a)
				}
			}
		}
		cfg.Launchers = append(cfg.Launchers, Launcher{Name: l.Name, Command: cmd})
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return cfg, nil
}

func validUntracked(v string) bool { return v == "normal" || v == "no" }

// expandCommand expands a command that is either a bare executable name
// (looked up in PATH later) or a path.
func expandCommand(v, baseDir string, getenv func(string) string) (string, error) {
	if !strings.ContainsRune(v, '/') && !strings.HasPrefix(v, "~") && !strings.Contains(v, "${") {
		return v, nil
	}
	return expandPath(v, baseDir, getenv)
}

// expandPath expands a leading ~ and ${NAME} references and resolves relative
// paths against baseDir. Undefined variables are errors.
func expandPath(v, baseDir string, getenv func(string) string) (string, error) {
	expanded, err := expandVars(v, getenv)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		if baseDir == "" {
			wd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			baseDir = wd
		}
		expanded = filepath.Join(baseDir, expanded)
	}
	return filepath.Clean(expanded), nil
}

// expandWorkspace expands variables and ~ but keeps a relative path relative;
// the inventory resolves it against the repository.
func expandWorkspace(v string, getenv func(string) string) (string, error) {
	if v == "" {
		return "", nil
	}
	expanded, err := expandVars(v, getenv)
	if err != nil {
		return "", err
	}
	return filepath.Clean(expanded), nil
}

// expandVars expands ${NAME} references and a leading ~.
func expandVars(v string, getenv func(string) string) (string, error) {
	if strings.Contains(v, "$") && !validVarSyntax(v) {
		return "", fmt.Errorf("%q: only ${NAME} variable references are supported", v)
	}
	var missing []string
	expanded := os.Expand(v, func(name string) string {
		val := getenv(name)
		if val == "" {
			missing = append(missing, name)
		}
		return val
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("%q: undefined environment variable %s", v, strings.Join(missing, ", "))
	}
	if expanded == "~" || strings.HasPrefix(expanded, "~/") {
		home := getenv("HOME")
		if home == "" {
			return "", fmt.Errorf("%q: HOME is not set", v)
		}
		expanded = filepath.Join(home, strings.TrimPrefix(expanded, "~"))
	}
	return expanded, nil
}

// validVarSyntax accepts only ${NAME} forms.
func validVarSyntax(v string) bool {
	for i := 0; i < len(v); i++ {
		if v[i] != '$' {
			continue
		}
		if i+1 >= len(v) || v[i+1] != '{' {
			return false
		}
		end := strings.IndexByte(v[i:], '}')
		if end < 0 {
			return false
		}
		i += end
	}
	return true
}
