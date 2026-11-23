# ADR 0005: Terminal handover and job coordination

Status: Accepted

## Context

Interactive tools need the real terminal, while Git jobs may continue
running. Bubble Tea does not process messages during a foreground child.
Linked worktrees share a store and can contend over Git data.

## Decision

Run lazygit, interactive fetch retry, and launchers as synchronous
foreground children through Bubble Tea's `tea.ExecProcess`. The working
directory is the worktree root; stdin, stdout, and stderr are the terminal.
Use the user's environment without the redirecting `GIT_*` variables listed
in [ADR 0003](0003-git-cli-and-process-isolation.md). Interactive children
are not bound to a cancellable context. headquarters emits no terminal
escape sequences of its own.

While a child owns the terminal, the TUI does not render or read keys.
Batch jobs continue without terminal access. Workers never block on the UI:
the scheduler buffers their events in an unbounded queue and a forwarder
delivers them after handover. Do not start a new job for the open store;
refuse to open a store with an active mutating job.

Locks are keyed by common Git directory, not worktree. At most one mutating
operation runs per store; different stores can fetch concurrently within
`fetch_parallelism`. An interactive session locks its store against new
dashboard operations. Cancellation prevents new jobs and terminates
running process groups.

After handover, restore terminal mode and size and inspect all entries of
the store regardless of child exit status; retain filter and selection.
Exit with a fatal error if terminal restoration fails.

Asynchronous completion must never open a confirmable dialog: a key meant
for the previous view could otherwise confirm an action. Report completion
with a message and wait for an explicit key. An update after its fetch was
already confirmed and opens no dialog; it appears in the operations view
only if that view is open.

Inspections run concurrently within `inspection_parallelism`, with a
timeout each, and apply incrementally. Each UI batch registers its event
handler. Drop events for batches that no longer matter, such as a replaced
inventory or superseded history request. A job reports failure through its
error even when it carries a partial result. After operations or handovers,
inspect all entries of the affected stores. A full rebuild (`R`) is separate
because it can add or remove discovered repositories.

Launchers use argument arrays without a shell. They start in the worktree
root; `{path}` and `{workspace}` must be complete arguments. Opening a
remote in a browser derives an HTTPS URL from the primary remote: configured
`remote_name`, else `origin`, else the alphabetically first remote. For SSH
remotes, assume the web interface uses the same host and path.

## Consequences

The lazygit process boundary avoids dependence on its internal, non-public
packages and uses the user's version and configuration. It also makes
terminal ownership explicit. Missing lazygit is detected at startup and
reported on `Enter`, without making the dashboard unusable. The event queue
allows workers to finish even while the UI is unable to receive messages.
