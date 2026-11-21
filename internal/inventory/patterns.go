package inventory

import "github.com/bmatcuk/doublestar/v4"

// matcher applies include and exclude patterns to slash-separated paths
// relative to a discovery root. Matching is case-sensitive.
type matcher struct {
	include []string
	exclude []string
}

func (m matcher) excluded(rel string) bool {
	for _, p := range m.exclude {
		if ok, _ := doublestar.Match(p, rel); ok {
			return true
		}
	}
	return false
}

func (m matcher) included(rel string) bool {
	if len(m.include) == 0 {
		return true
	}
	for _, p := range m.include {
		if ok, _ := doublestar.Match(p, rel); ok {
			return true
		}
	}
	return false
}
