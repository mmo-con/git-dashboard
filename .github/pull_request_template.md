<!--
Use an imperative title that names the outcome. Reference the issue, for
example "Closes #45". Delete the sections and items that do not apply.
-->

## Problem

<!--
What is wrong or missing, and why it matters: observed behavior, the
evidence (test, Git command, review finding), and the affected ADRs.
-->

## Change

<!--
What changes and how, per commit if the pull request has several. Name the
ADRs that are added, amended, or corrected.
-->

## Behavior changes

<!--
Changes that users or scripts notice: keys, configuration, the output and
exit status of `headquarters status`, performance, or Git commands that
headquarters runs. Write "None" if there are none.
-->

## Verification

<!--
How the change was verified: new or changed tests, and whether they failed
before the fix; manual checks; Git versions or platforms checked beyond CI.
-->

## Checklist

- [ ] Tests for new or fixed behavior; `make lint` and `go test -race ./...` pass
- [ ] Design decisions follow the accepted ADRs, or an ADR is added or amended (see `docs/adr/README.md`)
- [ ] Documentation updated as listed in `AGENTS.md`: configuration, keys and symbols, ADRs, package structure, README
- [ ] Text from repositories is passed through `domain.Printable`, remote URLs through `domain.RedactURL`
