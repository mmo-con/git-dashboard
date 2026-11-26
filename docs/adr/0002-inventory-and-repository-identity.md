# ADR 0002: Inventory and repository identity

Status: Accepted

## Context

Users need deterministic control over important repositories without
maintaining an exhaustive list. Linked worktrees have distinct working
states but share Git data.

## Decision

Use a hybrid inventory: explicit `[[repositories]]` entries and bounded
`[[discovery]]` below roots. Explicit entries win; `enabled = false`
suppresses an individual repository. The detailed configuration and
precedence rules are in [the configuration reference](../configuration.md).

Discovery must be bounded. An unrestricted scan would traverse dependency
and build trees, pick up unrelated repositories, change its result when
something is cloned below a root, and make startup cost unpredictable.

A command-line path replaces the configured inventory with discovery below
that path using discovery defaults, but does not replace other settings. The
path remains in use after `R`.

The canonical worktree root is an entry's identity: expand `~` and
`${NAME}`, make the path absolute, and resolve symbolic links. Deduplication
also uses `os.SameFile` for filesystem aliases and case-insensitive
filesystems. An explicitly configured subdirectory is a `config error`
naming the actual root; it is not normalized silently.

Linked worktrees are separate entries. Entries with the same common Git
directory form a **repository store**. The store is the unit of locking and
fetching; see [ADR 0005](0005-terminal-handover-and-job-coordination.md)
and [ADR 0004](0004-batch-fetch-and-safe-update.md). Bare repositories are
not supported as entries.

The local path, not a URL, identifies an existing checkout. A configured
remote is an assertion: report a mismatch, never fix it. Repositories can
have several remotes, different URLs can name the same repository, and host
aliases depend on the environment. Automatic cloning is not implemented; a
future clone would require an explicit user action through the Git CLI.

Families are assigned explicitly or default to `Ungrouped`; they are not
derived from directory names. When several roots find a repository, the
first root supplies defaults and a warning is reported.

## Consequences

- The walk stops at `max_depth` below the root, whose depth is zero.
- A directory with a `.git` directory or file is a candidate and is
  validated through Git. Other directories are not errors.
- The walk does not descend into repositories. Nested repositories,
  submodules, and vendored checkouts require explicit entries. Bare
  repositories lack a `.git` entry and are not discovered.
- Doublestar patterns match slash-separated paths relative to the root,
  case-sensitively. Excluded directories are pruned; a pattern ending in
  `/**` also matches the directory itself.
- Symlinks are followed only when enabled; previously visited directories
  are skipped by device and inode.

Validation outcomes:

| Outcome | Explicit entry | Discovered candidate |
|---|---|---|
| Valid worktree root | available | included |
| Subdirectory of worktree | `config error` naming the root | not applicable |
| Bare repository | `unsupported: bare repository` | never a candidate |
| Not a repository | `not a repository` | ignored |
| Missing path | `missing` | not applicable |
| Untrusted owner | `untrusted owner`, hint to `safe.directory` | listed as `untrusted owner` |
| Git crashed, timed out, or did not start | `validation failed` | listed as `validation failed` |

Only a Git exit status classifies a candidate as not a repository. Without
one, the outcome is unknown, and a discovered candidate must not disappear
silently (see [ADR 0006](0006-domain-state-and-errors.md)). The starting
configuration generator reports such candidates as warnings.
