// Package initconfig scans directories once and generates a starting
// inventory configuration.
package initconfig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"

	"github.com/mmo-con/headquarters/internal/config"
	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/git"
	"github.com/mmo-con/headquarters/internal/inventory"
)

// Options control a generation run. Roots are absolute directories.
type Options struct {
	Roots    []string
	MaxDepth int
	Home     string
	Command  string
}

// Result is a generated configuration.
type Result struct {
	Config       []byte
	Warnings     []string
	Repositories int
}

// rootPlan is the scan result of one root.
type rootPlan struct {
	root   string
	isRepo bool
	groups []string
	nested []repositoryEntry
}

type discoveryEntry struct {
	Root     string   `toml:"root"`
	MaxDepth int      `toml:"max_depth"`
	Family   string   `toml:"family"`
	Exclude  []string `toml:"exclude,omitempty"`
}

type repositoryEntry struct {
	Path   string `toml:"path"`
	Family string `toml:"family"`
}

// Generate scans the roots and returns a configuration.
func Generate(ctx context.Context, opts Options, v inventory.Validator) (Result, error) {
	var res Result
	var plans []rootPlan
	roots, warns := distinctRoots(opts.Roots)
	res.Warnings = append(res.Warnings, warns...)
	for _, root := range roots {
		if err := representable(root); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("root %s: %v; skipped", root, err))
			continue
		}
		plan, n, warns := scanRoot(ctx, root, opts.MaxDepth, v)
		res.Repositories += n
		res.Warnings = append(res.Warnings, warns...)
		plans = append(plans, plan)
	}
	for i, w := range res.Warnings {
		res.Warnings[i] = domain.Printable(w)
	}
	if res.Repositories == 0 {
		return Result{Warnings: res.Warnings}, ErrNoRepositories
	}
	res.Config = render(opts, plans, res.Warnings)
	return res, nil
}

// distinctRoots canonicalizes the roots and drops every root that equals,
// contains, or lies inside an earlier one, so that no repository is found by
// two discovery entries.
func distinctRoots(roots []string) (kept []string, warnings []string) {
	inside := func(dir, parent string) bool {
		return dir == parent || strings.HasPrefix(dir, strings.TrimSuffix(parent, string(filepath.Separator))+string(filepath.Separator))
	}
	for _, r := range roots {
		c := inventory.Canonical(r)
		overlap := ""
		for _, k := range kept {
			if inside(c, k) || inside(k, c) {
				overlap = k
				break
			}
		}
		if overlap != "" {
			warnings = append(warnings, fmt.Sprintf("root %s overlaps root %s; skipped", r, overlap))
			continue
		}
		kept = append(kept, c)
	}
	return kept, warnings
}

func scanRoot(ctx context.Context, root string, maxDepth int, v inventory.Validator) (plan rootPlan, repos int, warnings []string) {
	plan.root = root
	paths, warnings := inventory.Discover(ctx, config.Discovery{Root: root, MaxDepth: maxDepth, Include: []string{"**"}})
	groups := map[string]bool{}
	for _, p := range paths {
		val, ok := validate(ctx, v, p)
		if !ok {
			continue
		}
		repos++
		family := string(domain.UngroupedFamily)
		depth := 0
		if p == root {
			plan.isRepo = true
		} else if rel, err := filepath.Rel(root, p); err == nil {
			parts := strings.Split(filepath.ToSlash(rel), "/")
			depth = len(parts)
			if depth > 1 {
				if _, seen := groups[parts[0]]; !seen {
					dir := filepath.Join(root, parts[0])
					err := representable(dir)
					if err != nil {
						warnings = append(warnings, fmt.Sprintf("directory %s: %v; its repositories stay Ungrouped", dir, err))
					}
					groups[parts[0]] = err == nil
				}
				if groups[parts[0]] {
					family = parts[0]
				}
			}
		}
		nested, warns := findNested(ctx, v, val.Location, p, depth, maxDepth)
		warnings = append(warnings, warns...)
		for _, n := range nested {
			if err := representable(n); err != nil {
				warnings = append(warnings, fmt.Sprintf("nested repository %s: %v; skipped", n, err))
				continue
			}
			plan.nested = append(plan.nested, repositoryEntry{Path: n, Family: family})
		}
	}
	for g, ok := range groups {
		if ok {
			plan.groups = append(plan.groups, g)
		}
	}
	sort.Strings(plan.groups)
	sort.Slice(plan.nested, func(i, j int) bool { return plan.nested[i].Path < plan.nested[j].Path })
	return plan, repos, warnings
}

// validate reports whether discovery would include path.
func validate(ctx context.Context, v inventory.Validator, path string) (git.Validation, bool) {
	ctx, cancel := context.WithTimeout(ctx, config.DefaultInspectionTimeout)
	defer cancel()
	val := v.Validate(ctx, path)
	return val, val.Availability == domain.Available || val.Availability == domain.UntrustedOwner
}

// findNested returns the repositories inside the worktree of the repository
// at dir, which lies depth levels below its root, down to maxDepth levels
// below the root. It does not descend into nested repositories and skips
// submodules, whose Git data lives in the modules directory of the parent.
func findNested(ctx context.Context, v inventory.Validator, parent domain.Location, dir string, depth, maxDepth int) (nested, warnings []string) {
	modules := filepath.Join(parent.CommonDir, "modules") + string(filepath.Separator)
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth >= maxDepth || ctx.Err() != nil {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("nested repositories: %v", err))
			return
		}
		for _, e := range entries {
			if !e.IsDir() || e.Name() == ".git" {
				continue
			}
			child := filepath.Join(dir, e.Name())
			if _, err := os.Lstat(filepath.Join(child, ".git")); err != nil {
				walk(child, depth+1)
				continue
			}
			val, ok := validate(ctx, v, child)
			if ok && !strings.HasPrefix(val.Location.CommonDir+string(filepath.Separator), modules) {
				nested = append(nested, child)
			}
		}
	}
	walk(dir, depth)
	return nested, warnings
}

func render(opts Options, plans []rootPlan, warnings []string) []byte {
	var buf bytes.Buffer
	if opts.Command != "" {
		fmt.Fprintf(&buf, "# Generated by: %s\n", domain.Printable(opts.Command))
	}
	buf.WriteString(header)
	if len(warnings) > 0 {
		buf.WriteString("#\n")
	}
	for _, w := range warnings {
		fmt.Fprintf(&buf, "# warning: %s\n", w)
	}
	buf.WriteString("\nversion = 1\n")
	ungrouped := string(domain.UngroupedFamily)
	short := func(path string) string { return shorten(path, opts.Home) }
	for _, p := range plans {
		if p.isRepo {
			fmt.Fprintf(&buf, "\n# The root %s is itself a repository.\n", domain.Printable(short(p.root)))
			writeEntry(&buf, "repositories", repositoryEntry{Path: short(p.root), Family: ungrouped})
			continue
		}
		fmt.Fprintf(&buf, "\n# Repositories below %s.\n"+
			"# Each first-level directory that contains repositories is a family;\n"+
			"# all other repositories are %s.\n", domain.Printable(short(p.root)), ungrouped)
		var exclude []string
		for _, g := range p.groups {
			exclude = append(exclude, escapeGlob(g)+"/**")
		}
		writeEntry(&buf, "discovery", discoveryEntry{Root: short(p.root), MaxDepth: opts.MaxDepth, Family: ungrouped, Exclude: exclude})
		for _, g := range p.groups {
			buf.WriteString("\n")
			writeEntry(&buf, "discovery", discoveryEntry{Root: short(filepath.Join(p.root, g)), MaxDepth: opts.MaxDepth - 1, Family: g})
		}
	}
	first := true
	for _, p := range plans {
		for _, n := range p.nested {
			if first {
				buf.WriteString(nestedComment)
				first = false
			}
			n.Path = short(n.Path)
			entry := bytes.NewBufferString("\n")
			writeEntry(entry, "repositories", n)
			for _, line := range strings.SplitAfter(entry.String(), "\n") {
				if line != "" && line != "\n" {
					buf.WriteString("# ")
				}
				buf.WriteString(line)
			}
		}
	}
	return buf.Bytes()
}

const header = `# Starting configuration for headquarters. Review the families, then save
# it as ~/.config/headquarters/config.toml or select it with --config.
# All options are described in docs/configuration.md.
`

const nestedComment = `
# Nested repositories lie inside another repository and are not discovered.
# Remove the leading "# " of an entry to include it.
`

// shorten writes a path below home as ~/...
func shorten(path, home string) string {
	if home == "" || home == string(filepath.Separator) {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		if rel == "." {
			return "~"
		}
		return "~/" + filepath.ToSlash(rel)
	}
	return path
}

// representable reports why path cannot be written to a configuration file:
// the configuration expands every "$" and requires valid UTF-8.
func representable(path string) error {
	switch {
	case !utf8.ValidString(path):
		return fmt.Errorf("not valid UTF-8")
	case strings.Contains(path, "$"):
		return fmt.Errorf(`contains "$", which the configuration cannot escape`)
	}
	return nil
}

// escapeGlob escapes the doublestar metacharacters in a directory name.
func escapeGlob(name string) string {
	var b strings.Builder
	for _, r := range name {
		if strings.ContainsRune(`\*?[]{}`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func writeEntry(buf *bytes.Buffer, table string, entry any) {
	out, err := toml.Marshal(map[string]any{table: []any{entry}})
	if err != nil {
		panic(err) // entries contain only strings and integers
	}
	buf.Write(out)
}

// ErrNoRepositories reports that the roots contain no repository.
var ErrNoRepositories = errors.New("no repositories found")

// WriteFile writes data to path through a temporary file and a rename, and
// creates missing parent directories. An existing file is replaced only with
// force; if path is a symbolic link, its target is replaced.
func WriteFile(path string, data []byte, force bool) error {
	target := path
	if fi, err := os.Lstat(path); err == nil {
		if !force {
			return fmt.Errorf("%s: %w; use --force to replace it", path, os.ErrExist)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			if target, err = filepath.EvalSymlinks(path); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".headquarters-init-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}
