package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mmo-con/headquarters/internal/domain"
)

// Validation is the outcome of validating a candidate path.
type Validation struct {
	Availability domain.Availability
	Location     domain.Location
	Err          error
}

var markerNames = []string{
	"MERGE_HEAD", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD",
	"REVERT_HEAD", "BISECT_LOG", "FETCH_HEAD",
}

// Validate classifies path and resolves its Git locations with a single Git
// process. path must already be absolute.
func (c *Client) Validate(ctx context.Context, path string) Validation {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Validation{Availability: domain.Missing, Err: errors.New("configured repository is missing")}
	}
	if err != nil {
		return Validation{Availability: domain.NotARepository, Err: err}
	}
	if !info.IsDir() {
		return Validation{Availability: domain.NotARepository, Err: errors.New("path is not a directory")}
	}
	if _, err := os.Lstat(filepath.Join(path, ".git")); err != nil {
		if looksBare(path) {
			return Validation{Availability: domain.BareRepository, Err: errors.New("bare repositories are not supported")}
		}
		if root, ok := enclosingWorktree(path); ok {
			return Validation{Availability: domain.NotWorktreeRoot,
				Err: fmt.Errorf("path is inside the repository %s; configure the worktree root instead", root)}
		}
		return Validation{Availability: domain.NotARepository, Err: errors.New("not a Git repository")}
	}

	args := []string{"rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir", "--show-toplevel"}
	for _, m := range markerNames {
		args = append(args, "--git-path", m)
	}
	res, err := c.run.run(ctx, path, true, args...)
	if err != nil {
		if ownedByOtherUser(path) {
			return Validation{Availability: domain.UntrustedOwner,
				Err: fmt.Errorf("repository belongs to another user; see git config safe.directory: %w", err)}
		}
		return Validation{Availability: domain.NotARepository, Err: err}
	}
	lines := strings.Split(strings.TrimSuffix(string(res.stdout), "\n"), "\n")
	if len(lines) != 3+len(markerNames) {
		return Validation{Availability: domain.NotARepository,
			Err: errors.New("unexpected rev-parse output; paths containing newlines are not supported")}
	}
	loc := domain.Location{
		GitDir:       lines[0],
		CommonDir:    canonical(lines[1]),
		WorktreeRoot: lines[2],
		Markers: domain.Markers{
			MergeHead: lines[3], RebaseMerge: lines[4], RebaseApply: lines[5],
			CherryPickHead: lines[6], RevertHead: lines[7], BisectLog: lines[8], FetchHead: lines[9],
		},
	}
	if !samePath(loc.WorktreeRoot, path) {
		return Validation{Availability: domain.NotWorktreeRoot, Location: loc,
			Err: fmt.Errorf("path is inside the repository %s; configure the worktree root instead", loc.WorktreeRoot)}
	}
	loc.WorktreeRoot = path
	return Validation{Availability: domain.Available, Location: loc}
}

// looksBare detects a bare repository through the file system only, because
// Git refuses -C access to bare repositories under safe.bareRepository=explicit.
func looksBare(path string) bool {
	head, err := os.Stat(filepath.Join(path, "HEAD"))
	if err != nil || !head.Mode().IsRegular() {
		return false
	}
	for _, d := range []string{"objects", "refs"} {
		fi, err := os.Stat(filepath.Join(path, d))
		if err != nil || !fi.IsDir() {
			return false
		}
	}
	return true
}

// enclosingWorktree finds a parent directory that contains a .git entry.
func enclosingWorktree(path string) (string, bool) {
	dir := filepath.Dir(path)
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ai, err1 := os.Stat(a)
	bi, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ai, bi)
}

func canonical(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}
