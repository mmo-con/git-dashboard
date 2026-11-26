# ADR 0014: Dependency security and Go toolchain

Status: Accepted

## Context

Release binaries link the Go standard library of the toolchain that builds
them. CI installed Go from the `go` line of `go.mod`, which is a minimum,
not a recommendation: releases up to v1.0.3 were built with Go 1.25.0 and
contained standard-library vulnerabilities fixed in later patch releases.
Go publishes security fixes only for its two newest minor versions, and new
advisories appear without changes to the code. Dependabot alerts cover
modules, not the toolchain, and do not check whether vulnerable code is
reachable.

## Decision

The `go` line of `go.mod` is the oldest Go minor version that still receives
security fixes, with patch level `.0`. It is raised when a new Go minor
version is released. The test matrix of
[ADR 0008](0008-compatibility-performance-and-testing.md) builds with this
minimum, so it shows that the stated requirement holds.

Releases are built with the newest stable Go release, including its latest
patch, not with the minimum from `go.mod`. The release build and the
vulnerability check do not use the Actions cache, so no state from earlier
runs reaches the release binaries and new Go versions add no cache entries.

`make vuln` runs the latest govulncheck in source mode, which reports only
vulnerabilities the code reaches, in modules and in the standard library of
the Go on `PATH`. CI runs it with the release toolchain on pushes to `main`,
version tags, and pull requests, daily on a schedule, and before a release
draft. A finding fails the job and blocks the release draft.

Dependabot proposes updates for Go modules and GitHub Actions weekly,
grouping minor and patch updates; Dependabot security updates stay enabled.
govulncheck decides the urgency: a finding it reports is fixed promptly and
released as a patch version, because only the latest release receives
security fixes. Other updates ship with the next regular release.

## Consequences

Building from source requires the minimum Go version. Older Go versions
that support toolchain switching download a newer toolchain automatically
unless `GOTOOLCHAIN=local` is set.

A new advisory can fail CI for a change that did not cause it. The daily
scheduled run reports such findings even when nothing is pushed; it skips
the test matrix.

govulncheck is not a module dependency; `make vuln` runs it with
`go run`, so its version is not pinned.
