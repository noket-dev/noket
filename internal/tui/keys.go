package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
)

// keyMap holds the browse-mode bindings. The modal bindings only feed the help
// line; huh handles the keys themselves.
type keyMap struct {
	Open    key.Binding
	Add     key.Binding
	Edit    key.Binding
	Test    key.Binding
	Remove  key.Binding
	Up      key.Binding
	Down    key.Binding
	Refresh key.Binding
	Quit    key.Binding
	Next    key.Binding
	Confirm key.Binding
	Cancel  key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Open:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		Add:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add")),
		Edit:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Test:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "test")),
		Remove:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "remove")),
		Up:      key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "up")),
		Down:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "down")),
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Next:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "next")),
		Confirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Open, k.Add, k.Edit, k.Test, k.Remove, k.Up, k.Down, k.Refresh, k.Quit}
}

// modelBindings are huh's multi-select keys. The form handles them; the TUI
// draws them so the picker says how to select, not just how to confirm.
func modelBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "move")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// detailBindings are the keys that work inside the details popup: they close
// it, or close it and run the matching browse action.
func detailBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter", "esc"), key.WithHelp("enter/esc", "close")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "test")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "remove")),
	}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

// helpBindings adapts an ad-hoc list of bindings to bubbles/help.
type helpBindings []key.Binding

func (h helpBindings) ShortHelp() []key.Binding { return h }

func (h helpBindings) FullHelp() [][]key.Binding { return [][]key.Binding{h} }

// tableKeys is the table's own key map with every letter binding removed: the
// defaults take u/d/f/b/g/G, which the TUI needs for add/edit/remove/up/down.
func tableKeys() table.KeyMap {
	km := table.DefaultKeyMap()
	km.PageUp = key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up"))
	km.PageDown = key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down"))
	km.HalfPageUp = key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "½ page up"))
	km.HalfPageDown = key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "½ page down"))
	km.GotoTop = key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "go to start"))
	km.GotoBottom = key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "go to end"))
	return km
}
