package domain

import (
	"strings"
	"time"
)

type OperationType string

const (
	OperationFetch  OperationType = "fetch"
	OperationUpdate OperationType = "update"
)

type OperationStatus string

const (
	StatusQueued    OperationStatus = "queued"
	StatusRunning   OperationStatus = "running"
	StatusSucceeded OperationStatus = "succeeded"
	StatusFailed    OperationStatus = "failed"
	StatusSkipped   OperationStatus = "skipped"
	StatusCancelled OperationStatus = "cancelled"
)

// Final reports whether the status will not change anymore.
func (s OperationStatus) Final() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusSkipped || s == StatusCancelled
}

type OperationResult struct {
	RepositoryID RepositoryID
	StoreID      StoreID
	Operation    OperationType
	Status       OperationStatus
	StartedAt    time.Time
	FinishedAt   time.Time
	ExitCode     *int
	Stderr       string
	Error        error
}

// NormalizeRemoteURL performs a conservative normalization for comparisons:
// surrounding whitespace, a trailing slash, and a trailing ".git" are removed.
func NormalizeRemoteURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".git")
	return u
}

// RedactURL removes user information from URLs such as
// https://user:token@host/path. SSH user names such as git@ are kept.
func RedactURL(u string) string {
	i := strings.Index(u, "://")
	if i < 0 || !strings.HasPrefix(strings.ToLower(u[:i]), "http") {
		return u
	}
	rest := u[i+3:]
	host := rest
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		host = rest[:slash]
	}
	at := strings.LastIndexByte(host, '@')
	if at < 0 {
		return u
	}
	return u[:i+3] + "***@" + rest[at+1:]
}
