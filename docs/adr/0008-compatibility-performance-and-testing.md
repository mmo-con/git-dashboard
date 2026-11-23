# ADR 0008: Compatibility, performance, and testing

Status: Accepted

## Context

The application depends on Git process behavior and must stay responsive
across a collection of repositories. Tests need to exercise real Git
behavior without relying on a user's Git configuration.

## Decision

Support Linux and macOS, not Windows. Require Git 2.34 or newer and check
the version at startup. Inspect 200 repositories on a local SSD within two
seconds of cold start, excluding network activity.

The process budget is three Git processes per entry and refresh, four for
detached HEAD, and one additional process with Git 2.34. Validation uses
one process per candidate on startup and rebuild. Recent commits for the
selected repository use one process when HEAD changes. The specific
commands are in [ADR 0003](0003-git-cli-and-process-isolation.md).

Use unit tests for configuration, inventory, parsing, eligibility, fetch
deduplication, environment sanitization, scheduler limits, and TUI update
logic. Drive the TUI model with key messages. Git behavior is tested with
real temporary repositories, isolated from the user's Git configuration.

Integration tests cover worktree states, operations in progress, HEAD and
upstream states, remotes, linked worktrees, bare and invalid repositories,
inventory precedence, symlink loops, macOS case-insensitive deduplication,
inspection without `index.lock`, terminal isolation, process-group
termination, and fast-forward acceptance and rejection. Expected states
come from the Git CLI itself.

Handover tests use a controlled helper executable to report working directory
and environment and return chosen exit codes; they verify that events
produced during handover arrive afterward. Test the real lazygit handover
manually for alternate screen, input, resize, redraw, and refresh.

## Consequences

A counting wrapper around real Git checks the process budget, and a
scheduler test checks actual parallelism. These deterministic properties
guard performance without depending on machine speed.

`make perf` benchmarks 200 generated repositories locally. It is not in CI
because process startup on shared runners, especially macOS, is too erratic
for the two-second target. CI runs lint and race-detector tests with coverage
on Linux with Git 2.34, Linux with the latest Git, and macOS. A version-tag
push creates a release draft only after the matrix passes; publication is
manual.

The POSIX shell installer is shipped with each release and pins downloads
to its own release tag. CI tests it on Linux and macOS with local archives
and simulated downloads, without contacting GitHub.
