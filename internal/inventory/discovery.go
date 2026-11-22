package inventory

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"syscall"

	"github.com/mmo-con/headquarters/internal/config"
)

type fileKey struct{ dev, ino uint64 }

func keyOf(fi os.FileInfo) (fileKey, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fileKey{}, false
	}
	return fileKey{dev: uint64(st.Dev), ino: uint64(st.Ino)}, true
}

// Discover walks a discovery root and returns the absolute paths of all
// directories that contain a .git entry and pass the include and exclude
// patterns. Excluded directories are pruned, and the walk never descends into
// a repository.
func Discover(ctx context.Context, d config.Discovery) (candidates []string, warnings []string) {
	info, err := os.Stat(d.Root)
	if err != nil {
		return nil, []string{fmt.Sprintf("discovery root %s: %v", d.Root, err)}
	}
	if !info.IsDir() {
		return nil, []string{fmt.Sprintf("discovery root %s is not a directory", d.Root)}
	}
	m := matcher{include: d.Include, exclude: d.Exclude}
	visited := map[fileKey]bool{}
	if k, ok := keyOf(info); ok {
		visited[k] = true
	}
	if hasGitEntry(d.Root) {
		return []string{d.Root}, nil
	}

	var walk func(dir, rel string, depth int)
	walk = func(dir, rel string, depth int) {
		if ctx.Err() != nil {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("discovery: %v", err))
			return
		}
		for _, e := range entries {
			name := e.Name()
			if name == ".git" {
				continue
			}
			full := filepath.Join(dir, name)
			childRel := name
			if rel != "" {
				childRel = path.Join(rel, name)
			}
			isDir := e.IsDir()
			if e.Type()&fs.ModeSymlink != 0 {
				if !d.FollowSymlinks {
					continue
				}
				fi, err := os.Stat(full)
				if err != nil || !fi.IsDir() {
					continue
				}
				isDir = true
			}
			if !isDir {
				continue
			}
			if m.excluded(childRel) {
				continue
			}
			if fi, err := os.Stat(full); err == nil {
				if k, ok := keyOf(fi); ok {
					if visited[k] {
						continue
					}
					visited[k] = true
				}
			}
			if hasGitEntry(full) {
				if m.included(childRel) {
					candidates = append(candidates, full)
				}
				continue
			}
			if depth+1 < d.MaxDepth {
				walk(full, childRel, depth+1)
			}
		}
	}
	walk(d.Root, "", 0)
	return candidates, warnings
}

func hasGitEntry(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}
