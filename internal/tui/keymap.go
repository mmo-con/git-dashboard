package tui

// Binding documents one key binding in the help view. docs/keybindings.md
// describes every binding in long form; a test checks that it lists each key
// of this list in the section of its group.
type Binding struct {
	Keys   string
	Action string
}

// BindingGroup is a titled list of bindings.
type BindingGroup struct {
	Title string
	// Right places the group in the right column of the wide help view.
	Right    bool
	Bindings []Binding
}

// Bindings lists every key binding of the application.
var Bindings = []BindingGroup{
	{Title: "Navigation", Bindings: []Binding{
		{"j / ↓", "next row"},
		{"k / ↑", "previous row"},
		{"g / G", "first / last row"},
		{"PgUp / PgDn", "page up / down"},
		{"h / ←", "tree: collapse family"},
		{"l / →", "tree: expand family"},
		{"[ / ]", "tree: previous / next family"},
	}},
	{Title: "View", Bindings: []Binding{
		{"/", "text filter, e.g. is:dirty"},
		{"1 … 7", "quick filter (see header)"},
		{"Esc", "clear filter / close dialog"},
		{"Tab / S-Tab", "next / previous family"},
		{"t", "search and choose a family"},
		{"v", "switch between list and family tree"},
		{"z", "tree: expand/collapse all"},
		{"s", "choose sorting"},
		{"r", "refresh visible repositories"},
		{"R", "reload config and inventory"},
		{"e", "errors and warnings"},
		{"b", "operations view"},
		{"?", "help"},
		{"q / Ctrl+C", "quit (asks while a batch runs)"},
	}},
	{Title: "Repository", Bindings: []Binding{
		{"Enter", "open in lazygit"},
		{"Enter", "expand or collapse a family row in the tree"},
		{"o", "launcher menu"},
		{"y", "copy path, branch, remote or web URL"},
	}},
	{Title: "Batch", Right: true, Bindings: []Binding{
		{"Space", "toggle mark (row or family)"},
		{"a / A", "mark all visible / clear marks"},
		{"f", "fetch marked, else row/family"},
		{"F", "fetch visible repositories"},
		{"u", "update marked, else row/family"},
		{"U", "update visible repositories"},
	}},
	{Title: "Update confirmation", Right: true, Bindings: []Binding{
		{"Enter / y", "fetch, then update"},
	}},
	{Title: "Operations view", Right: true, Bindings: []Binding{
		{"Enter", "open row in lazygit"},
		{"c", "cancel batch / pending update"},
		{"x", "retry failed operations"},
		{"u", "continue the update"},
		{"t", "retry failed fetch in terminal"},
		{"Esc / b", "close (batch continues)"},
	}},
	{Title: "Error view", Right: true, Bindings: []Binding{
		{"t", "fetch in terminal"},
	}},
}
