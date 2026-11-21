package inventory

import (
	"os"
	"path/filepath"
)

// Canonical returns the canonical form of an absolute path: symbolic links are
// resolved if the path exists, otherwise the cleaned path is returned.
func Canonical(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		if abs, err := filepath.Abs(r); err == nil {
			return abs
		}
		return r
	}
	return filepath.Clean(path)
}

// identity is used to deduplicate entries by canonical path and by device and
// inode, which handles case-insensitive file systems and path aliases.
type identity struct {
	path string
	info os.FileInfo
}

func newIdentity(path string) identity {
	c := Canonical(path)
	fi, err := os.Stat(c)
	if err != nil {
		fi = nil
	}
	return identity{path: c, info: fi}
}

func (a identity) same(b identity) bool {
	if a.path == b.path {
		return true
	}
	return a.info != nil && b.info != nil && os.SameFile(a.info, b.info)
}
