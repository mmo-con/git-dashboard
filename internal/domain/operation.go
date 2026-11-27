package domain

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type OperationType string

const (
	OperationFetch   OperationType = "fetch"
	OperationUpdate  OperationType = "update"
	OperationCleanup OperationType = "branch cleanup"
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
	return replaceUserInfo(u, "***@")
}

// StripURLUserInfo removes user information from HTTP(S) URLs entirely, so
// the result is still a usable URL. SSH user names such as git@ are kept.
func StripURLUserInfo(u string) string {
	return replaceUserInfo(u, "")
}

func replaceUserInfo(u, with string) string {
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
	return u[:i+3] + with + rest[at+1:]
}

// Printable makes text from external sources, such as commit subjects, Git
// error output, or directory names, safe for display in a terminal. Control
// characters, which could start escape sequences, and invalid UTF-8 are
// replaced by U+FFFD; a tab becomes a space, and so does a line break, so the
// result is always a single line.
func Printable(s string) string {
	safe := true
	for _, r := range s {
		if r == utf8.RuneError || unicode.IsControl(r) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n':
			return ' '
		case unicode.IsControl(r):
			return utf8.RuneError
		}
		return r
	}, s)
}
