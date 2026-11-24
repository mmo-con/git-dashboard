# ADR 0010: Copying values to the clipboard

Status: Accepted

## Context

Users paste the worktree path, branch name, or remote URL of a repository
into other terminals, tickets, and scripts. The detail panel shows these
values truncated, styled, and with redacted URLs. Clipboard access differs
by platform and session; terminals can also set the clipboard through the
OSC 52 escape sequence, but [ADR 0005](0005-terminal-handover-and-job-coordination.md)
states that headquarters emits no escape sequences of its own.

## Decision

`y` on a repository row opens a copy menu with the worktree path, the
checked-out branch, and the URL of the primary remote as defined in ADR
0005. A value that is unknown or does not exist is unavailable with its
reason; no placeholder is ever copied. An unborn HEAD has a branch name, a
detached HEAD has none.

Copy only through native tools: `pbcopy` on macOS; on Linux `wl-copy` when
`WAYLAND_DISPLAY` is set, then `xclip` and `xsel` when `DISPLAY` is set.
Tools are resolved in `PATH` when copying, and a missing, failing, or
timed-out tool falls back to the next one. Without a usable tool, copying
reports an error.

Clipboard tools are not interactive and get no terminal handover. They run
from a Bubble Tea command, without a shell, in their own session, with the
value on stdin and a timeout after which the process group is killed.
`wl-copy`, `xclip`, and `xsel` leave a process behind that serves the
selection and inherits stdout and stderr, so neither may be a pipe the
caller waits for: stdout is discarded and stderr goes to a temporary file.

Following [ADR 0006](0006-domain-state-and-errors.md), user information is
removed from HTTP(S) remote URLs before copying, not replaced by a redaction
marker, so the URL stays usable. Values with control characters, including
line breaks, or invalid UTF-8 are refused, because pasting them into a shell
could run commands.

OSC 52 was rejected for now: it would change ADR 0005, success cannot be
verified, and multiplexers need extra configuration. It is tracked as a
known limitation and implemented only on demand
([#30](https://github.com/mmo-con/headquarters/issues/30)).

## Consequences

- Copying does not work in SSH sessions or containers without a display.
- Success means the tool accepted the value; the message names the tool.
- Family rows, the operations view, and the error view have no copy action.
