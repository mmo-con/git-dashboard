// Package inventory combines explicit repositories and bounded discovery into
// the set of dashboard entries.
package inventory

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/mmo-con/git-dashboard/internal/config"
	"github.com/mmo-con/git-dashboard/internal/domain"
	"github.com/mmo-con/git-dashboard/internal/git"
)

// Validator classifies candidate paths.
type Validator interface {
	Validate(ctx context.Context, path string) git.Validation
}

// Result is the resolved inventory.
type Result struct {
	Repositories []domain.Repository
	Warnings     []string
}

type candidate struct {
	id        identity
	explicit  *config.Repository
	discovery *config.Discovery
}

// Build resolves the inventory according to the precedence rules: explicit
// entries always win, enabled = false suppresses a repository, and the first
// discovery root in configuration order supplies discovery defaults.
func Build(ctx context.Context, cfg *config.Config, v Validator) Result {
	var res Result
	var cands []*candidate
	find := func(id identity) *candidate {
		for _, c := range cands {
			if c.id.same(id) {
				return c
			}
		}
		return nil
	}

	for i := range cfg.Repositories {
		r := &cfg.Repositories[i]
		id := newIdentity(r.Path)
		if c := find(id); c != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("repository %s is configured more than once; using the first entry", r.Path))
			continue
		}
		cands = append(cands, &candidate{id: id, explicit: r})
	}

	for i := range cfg.Discovery {
		d := &cfg.Discovery[i]
		paths, warns := Discover(ctx, *d)
		res.Warnings = append(res.Warnings, warns...)
		for _, p := range paths {
			id := newIdentity(p)
			c := find(id)
			if c == nil {
				cands = append(cands, &candidate{id: id, discovery: d})
				continue
			}
			if c.discovery == nil {
				c.discovery = d
			} else if c.discovery != d {
				res.Warnings = append(res.Warnings, fmt.Sprintf("repository %s is found by discovery roots %s and %s; using %s",
					id.path, c.discovery.Root, d.Root, c.discovery.Root))
			}
		}
	}

	var included []*candidate
	for _, c := range cands {
		if c.explicit != nil && !c.explicit.Enabled {
			continue
		}
		included = append(included, c)
	}

	repos := make([]*domain.Repository, len(included))
	parallel := cfg.Git.InspectionParallelism
	if parallel < 1 {
		parallel = 1
	}
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i, c := range included {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c *candidate) {
			defer wg.Done()
			defer func() { <-sem }()
			vctx, cancel := context.WithTimeout(ctx, cfg.Git.InspectionTimeout)
			defer cancel()
			repos[i] = resolve(vctx, cfg, c, v)
		}(i, c)
	}
	wg.Wait()

	for _, r := range repos {
		if r != nil {
			res.Repositories = append(res.Repositories, *r)
		}
	}
	disambiguate(res.Repositories, explicitNames(included))
	return res
}

func resolve(ctx context.Context, cfg *config.Config, c *candidate, v Validator) *domain.Repository {
	r := &domain.Repository{
		ID:        domain.RepositoryID(c.id.path),
		Path:      c.id.path,
		Name:      filepath.Base(c.id.path),
		Family:    domain.UngroupedFamily,
		Untracked: domain.UntrackedMode(cfg.Git.Untracked),
		Source:    domain.InventorySourceDiscovered,
		BranchPolicy: domain.BranchPolicy{
			Enabled: cfg.Branches.Enabled,
			Pinned:  cfg.Branches.Pinned,
			Ignore:  cfg.Branches.Ignore,
			MaxAge:  cfg.Branches.MaxAge,
		},
	}
	ws := ""
	if c.discovery != nil {
		if c.discovery.Family != "" {
			r.Family = domain.FamilyID(c.discovery.Family)
		}
		ws = c.discovery.Workspace
	}
	if e := c.explicit; e != nil {
		r.Source = domain.InventorySourceExplicit
		if e.Name != "" {
			r.Name = e.Name
		}
		if e.Family != "" {
			r.Family = domain.FamilyID(e.Family)
		}
		if e.Workspace != "" {
			ws = e.Workspace
		}
		if e.Untracked != "" {
			r.Untracked = domain.UntrackedMode(e.Untracked)
		}
		if e.PinnedBranches != nil {
			r.BranchPolicy.Pinned = e.PinnedBranches
			r.BranchPolicy.ReportMissing = true
		}
		if e.Remote != "" {
			r.ExpectedRemote = &domain.ExpectedRemote{Name: e.RemoteName, URL: e.Remote}
		}
	}

	if ws != "" && !filepath.IsAbs(ws) {
		ws = filepath.Join(r.Path, ws)
	}
	r.Workspace = ws

	val := v.Validate(ctx, r.Path)
	r.Availability = val.Availability
	r.EntryError = val.Err
	r.Location = val.Location
	if val.Availability == domain.Available {
		r.StoreID = domain.StoreID(val.Location.CommonDir)
		return r
	}
	if c.explicit == nil && val.Availability != domain.UntrustedOwner {
		return nil // discovered non-repositories are ignored
	}
	return r
}
