package inventory

import (
	"path/filepath"
	"strings"

	"github.com/mmo-con/headquarters/internal/domain"
)

// explicitNames returns the paths of the candidates whose name is configured.
func explicitNames(cands []*candidate) map[string]bool {
	m := map[string]bool{}
	for _, c := range cands {
		if c.explicit != nil && c.explicit.Name != "" {
			m[c.id.path] = true
		}
	}
	return m
}

// disambiguate makes derived names unique: repositories that share their
// directory name, or collide with a configured name, are named by as many
// trailing path components as needed, for example hsm1/src and hsm2/src.
// Configured names are never changed.
func disambiguate(repos []domain.Repository, fixed map[string]bool) {
	taken := map[string]bool{}
	groups := map[string][]int{}
	var order []string
	for i, r := range repos {
		if fixed[r.Path] {
			taken[r.Name] = true
			continue
		}
		if _, ok := groups[r.Name]; !ok {
			order = append(order, r.Name)
		}
		groups[r.Name] = append(groups[r.Name], i)
	}
	for _, name := range order {
		idx := groups[name]
		if len(idx) < 2 && !taken[name] {
			continue
		}
		parts := make([][]string, len(idx))
		longest := 0
		for j, i := range idx {
			parts[j] = strings.Split(filepath.ToSlash(repos[i].Path), "/")
			longest = max(longest, len(parts[j]))
		}
		for k := 2; k <= longest; k++ {
			names := make([]string, len(idx))
			seen := map[string]bool{}
			unique := true
			for j, p := range parts {
				names[j] = strings.Join(p[max(len(p)-k, 0):], "/")
				if seen[names[j]] || taken[names[j]] {
					unique = false
				}
				seen[names[j]] = true
			}
			if unique || k == longest {
				for j, i := range idx {
					repos[i].Name = names[j]
				}
				break
			}
		}
	}
}
