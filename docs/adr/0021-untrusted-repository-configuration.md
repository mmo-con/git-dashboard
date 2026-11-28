# ADR 0021: Untrusted repository configuration

Status: Accepted

## Context

[ADR 0003](0003-git-cli-and-process-isolation.md) runs Git with the user's
configuration, which includes the `.git/config` of every repository.
Some settings name programs that Git starts. Two of them were reachable
without an explicit key: at startup, on `R`, on `r`, and in
`headquarters status`, headquarters inspects every repository of the
inventory, also every repository that discovery finds below a root.

- `core.fsmonitor` names a hook program that `git status` runs to ask for
  changed files. A review confirmed that `headquarters status` ran a script
  named there.
- `log.showSignature` makes `git log` verify signatures with `gpg.program`,
  `gpg.ssh.program`, or `gpg.x509.program`. The latest commit of a detached
  HEAD was read without `--no-show-signature`; the verification also
  prefixed its output to the commit ID.

A repository copied, unpacked, or cloned below a discovery root could thus
run a program as soon as headquarters started. `SECURITY.md` asks users to
work with trusted repositories, but starting the dashboard is not a
decision to trust every directory below a root.

## Decision

Every Git process that the client in `internal/git` starts in the
background gets `-c core.fsmonitor=`: an empty value disables the hook and
the built-in daemon in every supported Git version, from 2.34 on. Every
`git log` gets `--no-show-signature`, including the latest commit of a
detached HEAD:

```text
git log -1 --no-color --no-show-signature --format=%H%x00%h%x00%an%x00%ct%x00%s
```

Interactive children of the terminal handover are not changed: lazygit,
launchers, and the interactive fetch run as the user started them in that
repository ([ADR 0005](0005-terminal-handover-and-job-coordination.md)).

This amends ADR 0003, which inherits every setting: these two settings no
longer apply to background processes. The configuration itself is not
changed.

## Consequences

- Inspecting a repository runs no program that its configuration names.
- A user's own fsmonitor, such as the built-in daemon for a very large
  repository, does not speed up headquarters' `git status`. Inspection
  stays within the time budget of
  [ADR 0008](0008-compatibility-performance-and-testing.md) for usual
  repositories; lazygit and the user's own Git commands still use it.
- The fast-forward of the safe update rewrites the index without the
  fsmonitor extension. The user's next `git status` synchronizes with the
  fsmonitor once and restores the extension; no state is lost.
- Other programs named by repository configuration still run, but only after
  an explicit key for that repository: transports, `core.sshCommand`, and
  credential helpers during a fetch (`f`, `F`, `u`, `U`, `status --fetch`),
  and merge drivers named in the configuration and in `.gitattributes`
  during the containment check of the branch cleanup preview (`d`, `D`,
  [ADR 0019](0019-deleting-squash-and-rebase-merged-branches.md)).
  `SECURITY.md` describes this trust assumption.
- An integration test configures both settings with a program that leaves
  a marker and checks that inspection and fast-forward do not start it; CI
  runs it with Git 2.34 and the latest Git.
