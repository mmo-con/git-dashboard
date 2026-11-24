package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mmo-con/headquarters/internal/clipboard"
	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/launcher"
)

// copyItem is an entry of the copy menu. An entry with a reason is
// unavailable and cannot be copied.
type copyItem struct {
	title  string // shown in the menu
	label  string // used in messages
	value  string
	reason string
}

type copyDoneMsg struct {
	label string
	value string
	tool  string
	err   error
}

// copyItems returns the values of r that can be copied. Unknown or missing
// values are unavailable instead of being copied as placeholders.
func copyItems(r *domain.Repository) []copyItem {
	path := r.Location.WorktreeRoot
	if path == "" {
		path = r.Path
	}
	items := []copyItem{{title: "Path", label: "path", value: path}}

	branch := copyItem{title: "Branch", label: "branch"}
	switch {
	case !r.Head.Valid:
		branch.reason = "branch unknown"
	case r.Head.Value.Detached:
		branch.reason = "detached HEAD"
	case r.Head.Value.Branch == "":
		branch.reason = "branch unknown"
	default:
		branch.value = r.Head.Value.Branch
	}
	items = append(items, branch)

	remote := copyItem{title: "Remote URL", label: "remote URL"}
	web := copyItem{title: "Web URL", label: "web URL"}
	switch {
	case !r.Remotes.Valid:
		remote.reason, web.reason = "remotes unknown", "remotes unknown"
	case r.PrimaryRemote == nil:
		remote.reason, web.reason = "no remote configured", "no remote configured"
	default:
		remote.value = domain.StripURLUserInfo(r.PrimaryRemote.URL)
		// The same derivation as "Open remote in browser", so that an SSH
		// remote can be pasted into a browser.
		if u, ok := launcher.BrowserURL(r.PrimaryRemote.URL); ok {
			web.value = u
		} else {
			web.reason = "no web URL for this remote"
		}
	}
	items = append(items, remote, web)

	for i := range items {
		if items[i].reason != "" {
			continue
		}
		if err := clipboard.Check(items[i].value); err != nil {
			items[i].reason = err.Error()
		}
	}
	return items
}

// openCopy opens the copy menu on a repository row.
func (m *Model) openCopy() {
	if m.current() == nil {
		m.setMessage("select a repository to copy from")
		return
	}
	m.mode, m.overlayIdx = modeCopy, 0
}

func (m *Model) keyCopy(key string) tea.Cmd {
	r := m.current()
	if r == nil {
		m.mode = modeNormal
		return nil
	}
	items := copyItems(r)
	if m.moveOverlay(key, len(items)) {
		return nil
	}
	switch key {
	case "enter":
		it := items[min(m.overlayIdx, len(items)-1)]
		if it.reason != "" {
			m.setError(fmt.Sprintf("cannot copy %s: %s", it.label, it.reason))
			return nil
		}
		m.mode = modeNormal
		copyText := m.clipboard
		return func() tea.Msg {
			tool, err := copyText(context.Background(), it.value)
			return copyDoneMsg{label: it.label, value: it.value, tool: tool, err: err}
		}
	case "esc", "q", "y":
		m.mode = modeNormal
	}
	return nil
}

func (m *Model) finishCopy(msg copyDoneMsg) {
	if msg.err != nil {
		m.setError(fmt.Sprintf("cannot copy %s: %v", msg.label, msg.err))
		return
	}
	m.setMessage(fmt.Sprintf("copied %s %s (%s)", msg.label, msg.value, msg.tool))
}

func (m *Model) viewCopy(iw, rows int) (string, []string) {
	title := "Copy"
	r := m.current()
	if r == nil {
		return title, nil
	}
	title += " ─ " + r.Name
	var body []string
	for i, it := range copyItems(r) {
		text, style := fmt.Sprintf("  %-12s%s", it.title, it.value), m.theme.plain
		if it.reason != "" {
			text, style = fmt.Sprintf("  %-12s%s", it.title, it.reason), m.theme.dim
		}
		body = append(body, m.selectable(text, i == m.overlayIdx, iw, style))
	}
	return title, body
}
