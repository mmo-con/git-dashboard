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
		{"/", "text filter (name, path, is:dirty, is:behind, …)"},
		{"Esc", "clear the text filter or close the current overlay"},
		{"Tab / Shift+Tab", "next / previous family"},
		{"s", "choose sorting"},
		{"r", "refresh visible repository state"},
		{"R", "reload configuration, rebuild inventory, refresh all"},
		{"e", "show error details and warnings"},
		{"b", "show the operations view"},
		{"?", "help"},
		{"q / Ctrl+C", "quit"},
	}},
	{"Repository", []Binding{
		{"Enter", "open selected repository in lazygit"},
		{"o", "open launcher menu"},
	}},
	{"Batch", []Binding{
		{"Space", "toggle mark"},
		{"a", "mark all visible repositories"},
		{"A", "clear all marks"},
		{"f", "fetch marked repositories, or the repository under the cursor"},
		{"F", "fetch visible repositories"},
		{"u", "safe-update preview for marked or visible repositories"},
	}},
	{"Operations view", []Binding{
		{"c", "cancel the running batch"},
		{"x", "retry failed operations"},
		{"t", "retry the selected failed fetch in the terminal"},
		{"Esc / b", "close the operations view"},
	}},
	{"Error view", []Binding{
		{"t", "fetch the selected repository in the terminal"},
	}},
}
