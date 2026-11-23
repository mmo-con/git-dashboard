# ADR 0006: Domain state and errors

Status: Accepted

## Context

An inspection can fail or provide only partial information. Treating an
unknown value as a valid zero could present a dirty repository as clean or
an out-of-date branch as current.

## Decision

`domain.Known[T]` distinguishes an undetermined value from a valid zero.
An inspection failure invalidates state; it must not produce a clean
worktree or `Behind = 0` as a fallback. Derived values such as `Clean()`
and `Diverged()` are computed, not stored. Each inspection carries a
generation; an older result cannot overwrite a newer one.

Keep errors per repository with operation, repository, and cause. Bound
captured stderr; do not turn errors into plausible values. One broken,
hanging, or slow repository must not block the UI or other repositories.

Sanitize external text, including commit subjects, names, URLs, stderr,
and directory names, before display so it cannot inject terminal control
sequences. Redact user information in URLs. Configured executables must be
resolved at startup and be regular executable files.

## Consequences

The domain has no dependencies on UI, processes, or Git output formats.
The TUI consumes application services and domain models and never builds
Git commands. Unknown, unavailable, and zero states remain distinguishable
through presentation; see [ADR 0007](0007-user-interface-principles.md).
