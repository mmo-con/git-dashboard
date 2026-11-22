package git

import (
	"os"
	"time"

	"github.com/mmo-con/headquarters/internal/domain"
)

func exists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Lstat(path)
	return err == nil
}

// operationInProgress inspects marker files without starting a process.
func operationInProgress(m domain.Markers) domain.OperationInProgress {
	switch {
	case exists(m.RebaseMerge) || exists(m.RebaseApply):
		return domain.Rebasing
	case exists(m.MergeHead):
		return domain.Merging
	case exists(m.CherryPickHead):
		return domain.CherryPicking
	case exists(m.RevertHead):
		return domain.Reverting
	case exists(m.BisectLog):
		return domain.Bisecting
	}
	return domain.NoOperation
}

// fetchHeadTime returns the modification time of FETCH_HEAD, or nil if the
// worktree has never been fetched.
func fetchHeadTime(path string) domain.Known[*time.Time] {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return domain.KnownValue[*time.Time](nil)
		}
		return domain.Known[*time.Time]{}
	}
	t := fi.ModTime()
	return domain.KnownValue(&t)
}
