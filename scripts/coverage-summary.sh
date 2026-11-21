#!/bin/sh
# Prints a Markdown coverage report for a Go coverage profile: one row per
# package and the total statement coverage. Usage: coverage-summary.sh FILE
set -eu

profile=${1:?usage: coverage-summary.sh FILE}
module=$(go list -m)

echo "### Test coverage"
echo
echo "| Package | Coverage | Statements |"
echo "|---|---:|---:|"
# Test binaries of several packages report the same block; a block counts as
# covered if any of them executed it.
awk -v module="$module/" '
NR > 1 {
	block = $1
	stmts[block] = $2
	if ($3 > 0) hit[block] = 1
}
END {
	for (block in stmts) {
		pkg = block
		sub(/:.*/, "", pkg)
		sub(/\/[^\/]*$/, "", pkg)
		sub(module, "", pkg)
		total[pkg] += stmts[block]
		all += stmts[block]
		if (block in hit) {
			covered[pkg] += stmts[block]
			allCovered += stmts[block]
		}
	}
	for (pkg in total)
		printf "| `%s` | %.1f%% | %d / %d |\n", pkg, 100 * covered[pkg] / total[pkg], covered[pkg], total[pkg] | "sort"
	close("sort")
	if (all > 0)
		printf "| **Total** | **%.1f%%** | %d / %d |\n", 100 * allCovered / all, allCovered, all
}' "$profile"
