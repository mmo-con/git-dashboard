# ADR 0001: Product boundaries and technology

Status: Accepted, amended by [ADR 0018](0018-deleting-merged-branches.md)

## Context

headquarters gives users an overview of a curated collection of local Git
repositories and safe batch operations. Detailed work within a repository
already has a dedicated tool, lazygit.

## Decision

headquarters manages the repository collection; lazygit manages the
individual repository. headquarters does not implement a Git user interface,
create commits, resolve conflicts, implement Git protocols or credentials,
embed or fork lazygit, or run arbitrary commands across repositories.
Cloning and detailed single-repository work remain with other tools.
[ADR 0018](0018-deleting-merged-branches.md) allows deleting local branches
that are fully merged into the default branch, after a preview.
lazygit is the default; headquarters starts it only as an external process
in the worktree root, so another terminal Git client such as gitui or tig
can be configured in its place.

Use Go, Bubble Tea, and Lip Gloss. Use TOML for configuration, with unknown
fields rejected. Configuration syntax and precedence are specified in
[the configuration reference](../configuration.md).

## Consequences

The Go TUI ecosystem, bounded concurrency with `context`, process control,
and a single static binary fit the application. Rust was technically
excellent but more costly for this scope; Python and Node.js complicate
distribution. TOML supplies explicit types, comments, and repeated tables
without YAML's additional features.

The supported platforms and minimum Git version are defined by
[ADR 0008](0008-compatibility-performance-and-testing.md). Git integration
and process ownership are defined by
[ADR 0003](0003-git-cli-and-process-isolation.md) and
[ADR 0005](0005-terminal-handover-and-job-coordination.md).
