---
name: Bug report
about: Report wrong behavior, a crash, or documentation that does not match the behavior
title: ''
labels: bug
assignees: ''
---

<!--
Use a title that names the wrong behavior, for example "Update skips a
repository that is behind and clean". Delete the optional sections that do
not apply.

Report security vulnerabilities privately, not here; see SECURITY.md.

The output below can contain repository paths, remote URLs, and branch
names. Replace anything you do not want to publish.
-->

## What happened

<!--
The behavior you saw: the message, the symbol or state shown, the exit
status of `headquarters status`, or the wrong statement in the
documentation (file and section).
-->

## What you expected

<!--
The behavior you expected, and why: documentation, an ADR in docs/adr/, or
how Git itself behaves in that repository.
-->

## Steps to reproduce

1. <!-- Repository state, for example "a clone with a gone upstream and a stopped rebase" -->
2. <!-- Keys or command, for example "d, then Enter" or "headquarters status --fail-on dirty" -->
3. <!-- What happens -->

<!--
Git commands that create the repository state are the most useful:

```sh
git init demo && cd demo
…
```
-->

## Environment

- headquarters: <!-- output of `headquarters --version` -->
- Git: <!-- output of `git --version` -->
- OS: <!-- for example "Ubuntu 24.04" or "macOS 15.2" -->
- Terminal: <!-- for example "GNOME Terminal, tmux 3.4"; for display problems also the size -->

<!--
Optional sections:

## Configuration and inventory

The relevant part of config.toml and the output of `headquarters --check`.

## Git state

For the affected repository, the output of:

```sh
git status --porcelain=v2 --branch
git for-each-ref --format='%(refname) %(upstream:short) %(upstream:track)' refs/heads
```

## Error details

The text of the error view (`e`) or the operations view (`b`), or the
standard error of `headquarters status`.
-->
