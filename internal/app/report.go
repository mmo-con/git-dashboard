package app

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/mmo-con/headquarters/internal/domain"
)

// ReportSchema is the version of the status report. Adding fields keeps it;
// renaming, removing, or changing the meaning of a field increments it.
const ReportSchema = 1

// Report is the machine-readable status of the selected repositories. All
// text from repositories is sanitized and remote URLs are redacted; unknown
// state is null, never a clean or up-to-date value.
type Report struct {
	Schema       int                `json:"schema"`
	GeneratedAt  time.Time          `json:"generated_at"`
	Summary      Summary            `json:"summary"`
	Repositories []RepositoryReport `json:"repositories"`
}

// Summary counts the reported repositories in total, per state of
// StatusStates, and per health level.
type Summary struct {
	Total  int
	States map[string]int
	Health map[string]int
}

// MarshalJSON writes the state counts next to the total.
func (s Summary) MarshalJSON() ([]byte, error) {
	m := map[string]any{"total": s.Total, "health": s.Health}
	for k, v := range s.States {
		m[k] = v
	}
	return json.Marshal(m)
}

// RepositoryReport is the state of one repository.
type RepositoryReport struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Family       string `json:"family"`
	Availability string `json:"availability"`
	// Known reports whether the inspection determined the complete state.
	Known  bool    `json:"known"`
	Health string  `json:"health"`
	Branch *string `json:"branch"`
	// Head is "attached", "detached", or "unborn".
	Head *string `json:"head"`
	// Operation is "none" or the operation in progress, such as "merging".
	Operation         *string         `json:"operation"`
	Counts            *CountsReport   `json:"counts"`
	Upstream          *UpstreamReport `json:"upstream"`
	AttentionBranches *int            `json:"attention_branches"`
	Remote            *RemoteReport   `json:"remote"`
	LastFetch         *time.Time      `json:"last_fetch"`
	LastCommit        *CommitReport   `json:"last_commit"`
	// Fetch is the result of the fetch, null if the repository was not
	// fetched.
	Fetch *FetchReport `json:"fetch"`
	Error *string      `json:"error"`
}

type CountsReport struct {
	Staged    int `json:"staged"`
	Modified  int `json:"modified"`
	Deleted   int `json:"deleted"`
	Renamed   int `json:"renamed"`
	Conflicts int `json:"conflicts"`
	// Untracked is null if untracked files are not checked.
	Untracked *int `json:"untracked"`
	Stashed   *int `json:"stashed"`
}

type UpstreamReport struct {
	Name       *string `json:"name"`
	Configured bool    `json:"configured"`
	Gone       bool    `json:"gone"`
	Ahead      int     `json:"ahead"`
	Behind     int     `json:"behind"`
}

type RemoteReport struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type CommitReport struct {
	Hash      string    `json:"hash"`
	ShortHash string    `json:"short_hash"`
	Author    string    `json:"author"`
	Subject   string    `json:"subject"`
	Time      time.Time `json:"time"`
}

type FetchReport struct {
	Status string  `json:"status"`
	Error  *string `json:"error"`
}

// NewReport builds the report of s.
func NewReport(s *Status, now time.Time) Report {
	rep := Report{
		Schema:       ReportSchema,
		GeneratedAt:  now.UTC().Truncate(time.Second),
		Summary:      Summary{States: map[string]int{}, Health: map[string]int{}},
		Repositories: []RepositoryReport{},
	}
	for _, name := range StatusStates() {
		rep.Summary.States[name] = 0
	}
	for h := HealthUnknown; h <= HealthOK; h++ {
		rep.Summary.Health[h.String()] = 0
	}
	for i := range s.Repositories {
		r := &s.Repositories[i]
		rep.Summary.Total++
		for _, name := range StatusStates() {
			if s.Is(r, name) {
				rep.Summary.States[name]++
			}
		}
		rr := repositoryReport(s, r)
		rep.Summary.Health[rr.Health]++
		rep.Repositories = append(rep.Repositories, rr)
	}
	return rep
}

func repositoryReport(s *Status, r *domain.Repository) RepositoryReport {
	rr := RepositoryReport{
		Name:         clean(r.Name),
		Path:         clean(r.Path),
		Family:       clean(string(r.Family)),
		Availability: r.Availability.String(),
		Known: r.IsAvailable() && !r.Inspected.FinishedAt.IsZero() && r.Inspected.Err == nil &&
			r.Head.Valid && r.Operation.Valid && r.Worktree.Valid && r.Upstream.Valid,
		Health: s.Health(r).String(),
	}
	if r.Head.Valid {
		h := r.Head.Value
		kind := "attached"
		switch {
		case h.Detached:
			kind = "detached"
		case h.Unborn:
			kind = "unborn"
		}
		rr.Head = &kind
		if !h.Detached && h.Branch != "" {
			rr.Branch = ptr(clean(h.Branch))
		}
	}
	if r.Operation.Valid {
		op := "none"
		if r.Operation.Value != domain.NoOperation {
			op = r.Operation.Value.String()
		}
		rr.Operation = &op
	}
	if r.Worktree.Valid {
		w := r.Worktree.Value
		c := &CountsReport{Staged: w.Staged, Modified: w.Modified, Deleted: w.Deleted, Renamed: w.Renamed, Conflicts: w.Conflicts}
		if w.UntrackedChecked {
			c.Untracked = ptr(w.Untracked)
		}
		if r.Stashes.Valid {
			c.Stashed = ptr(r.Stashes.Value)
		}
		rr.Counts = c
	}
	if r.Upstream.Valid {
		u := r.Upstream.Value
		ur := &UpstreamReport{Configured: u.Configured, Gone: u.Gone, Ahead: u.Ahead, Behind: u.Behind}
		if u.Configured && u.Ref != "" {
			ur.Name = ptr(clean(u.Ref))
		}
		rr.Upstream = ur
	}
	if r.Branches.Valid {
		rr.AttentionBranches = ptr(len(r.Branches.Value))
	}
	if p := r.PrimaryRemote; p != nil {
		rr.Remote = &RemoteReport{Name: clean(p.Name), URL: clean(domain.RedactURL(p.URL))}
	}
	if r.LastFetch.Valid && r.LastFetch.Value != nil {
		t := r.LastFetch.Value.UTC()
		rr.LastFetch = &t
	}
	if r.LastCommit.Valid && r.LastCommit.Value != nil {
		c := r.LastCommit.Value
		rr.LastCommit = &CommitReport{Hash: c.ID, ShortHash: c.ShortID, Author: clean(c.Author), Subject: clean(c.Subject), Time: c.Time.UTC()}
	}
	var errs []string
	for _, err := range []error{r.EntryError, r.Inspected.Err} {
		if err != nil {
			errs = append(errs, clean(err.Error()))
		}
	}
	if res, ok := s.FetchResult(r); ok {
		f := &FetchReport{Status: string(res.Status)}
		if res.Error != nil {
			msg := clean(res.Error.Error())
			f.Error = &msg
			if res.Status == domain.StatusFailed {
				errs = append(errs, "fetch failed: "+msg)
			}
		}
		rr.Fetch = f
	}
	if len(errs) > 0 {
		rr.Error = ptr(strings.Join(errs, "; "))
	}
	return rr
}

func ptr[T any](v T) *T { return &v }

var urlPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s'"]+`)

// clean makes text from a repository safe to print and redacts the user
// information of URLs within it.
func clean(s string) string {
	return domain.Printable(urlPattern.ReplaceAllStringFunc(s, domain.RedactURL))
}
