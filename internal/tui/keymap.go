package tui

// Binding documents one key binding. The same list drives the help view and
// docs/keybindings.md.
type Binding struct {
	Keys   string
	Action string
}

// BindingGroup is a titled list of bindings.
type BindingGroup struct {
	Title    string
	Bindings []Binding
}

// Bindings lists every key binding of the application.
var Bindings = []BindingGroup{
	{"Navigation", []Binding{
		{"j / ↓", "next row"},
		{"k / ↑", "previous row"},
		{"g / G", "first / last row"},
		{"PgUp / PgDn", "page up / down"},
	}},
	{"View", []Binding{
		{"/", "text filter, e.g. is:dirty"},
		{"1 … 7", "quick filter (see header)"},
		{"Esc", "clear filter / close dialog"},
		{"Tab / S-Tab", "next / previous family"},
		{"t", "search and choose a family"},
		{"v", "switch between list and family tree"},
		{"s", "choose sorting"},
		{"r", "refresh visible repositories"},
		{"R", "reload config and inventory"},
		{"e", "errors and warnings"},
		{"b", "operations view"},
		{"?", "help"},
		{"q / Ctrl+C", "quit (asks while a batch runs)"},
	}},
	{"Repository", []Binding{
		{"Enter", "open in lazygit"},
		{"Enter", "expand or collapse a family row in the tree"},
		{"o", "launcher menu"},
	}},
	{"Batch", []Binding{
		{"Space", "toggle mark"},
		{"a / A", "mark all visible / clear marks"},
		{"f", "fetch marked, else current row"},
		{"F", "fetch visible repositories"},
		{"u", "update marked, else current row"},
		{"U", "update visible repositories"},
	}},
	{"Update confirmation", []Binding{
		{"Enter / y", "fetch, then update"},
	}},
	{"Operations view", []Binding{
		{"Enter", "open row in lazygit"},
		{"c", "cancel the batch or pending update"},
		{"x", "retry failed operations"},
		{"u", "continue the update"},
		{"t", "retry failed fetch in terminal"},
		{"Esc / b", "close (batch continues)"},
	}},
	{"Error view", []Binding{
		{"t", "fetch in terminal"},
	}},
}
