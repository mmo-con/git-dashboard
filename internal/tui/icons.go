package tui

// icons is one symbol set. Every symbol is accompanied by a number or text, so
// the meaning never depends on the symbol alone; the help view shows a legend.
type icons struct {
	clean, staged, modified, deleted, conflict, untracked string
	ahead, behind, upToDate, detached, noUpstream         string
	unavailable, failed, mark, mismatch, branch, queued   string
	branches, attention                                   string
	cursor, collapsed, expanded                           string
	spinner                                               []string
	barFull, barEmpty                                     string
}

var iconSets = map[string]icons{
	"unicode": {
		clean: "✓", staged: "✚", modified: "●", deleted: "−", conflict: "✘", untracked: "?",
		ahead: "↑", behind: "↓", upToDate: "✓", detached: "⌀", noUpstream: "∅",
		unavailable: "✗", failed: "✗", mark: "◆", mismatch: "≠", branch: "", queued: "…", branches: "⎇", attention: "!",
		cursor: "▌", collapsed: "▸", expanded: "▾",
		spinner: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		barFull: "█", barEmpty: "░",
	},
	"nerd": {
		clean: "\uf00c", staged: "\uf067", modified: "\uf111", deleted: "\uf068", conflict: "\uf071", untracked: "\uf128",
		ahead: "\uf062", behind: "\uf063", upToDate: "\uf00c", detached: "\uf417", noUpstream: "\uf127",
		unavailable: "\uf00d", failed: "\uf00d", mark: "\uf005", mismatch: "\uf0ec", branch: "\ue725 ", queued: "\uf252", branches: "\uf126", attention: "\uf06a",
		cursor: "▌", collapsed: "\uf105", expanded: "\uf107",
		spinner: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		barFull: "█", barEmpty: "░",
	},
	"ascii": {
		clean: "ok", staged: "+", modified: "*", deleted: "-", conflict: "!", untracked: "?",
		ahead: "^", behind: "v", upToDate: "ok", detached: "@", noUpstream: "-",
		unavailable: "x", failed: "x", mark: "*", mismatch: "!=", branch: "", queued: "...", branches: "br", attention: "~",
		cursor: ">", collapsed: "[+]", expanded: "[-]",
		spinner: []string{"|", "/", "-", "\\"},
		barFull: "#", barEmpty: ".",
	},
}

func iconSet(name string) icons {
	if s, ok := iconSets[name]; ok {
		return s
	}
	return iconSets["unicode"]
}

// legend explains the symbols in the help view.
func (ic icons) legend() []Binding {
	return []Binding{
		{ic.clean, "clean worktree / up to date"},
		{ic.staged + "n", "staged entries"},
		{ic.modified + "n", "modified entries"},
		{ic.deleted + "n", "deleted entries"},
		{ic.untracked + "n", "untracked entries"},
		{ic.conflict + "n", "conflicting entries"},
		{ic.ahead + "n / " + ic.behind + "n", "ahead of / behind upstream"},
		{ic.branches + "n", "other local branches need attention"},
		{ic.mismatch + "remote", "unexpected remote URL"},
		{ic.unavailable, "unavailable or failed"},
		{ic.mark, "marked"},
		{ic.conflict + ic.modified + ic.behind + ic.ahead + ic.attention + ic.clean, "column after the name: most important state"},
		{ic.attention, "other attention, e.g. remote mismatch or branches"},
		{ic.collapsed + " / " + ic.expanded, "collapsed / expanded family"},
	}
}
