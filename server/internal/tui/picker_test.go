package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPickerSelectsExactVersionBeforePartialMatches(t *testing.T) {
	p := newPicker("Версия", []pickItem{{label: "1.21.11", value: "1.21.11"}, {label: "1.21.10", value: "1.21.10"}, {label: "1.21.1", value: "1.21.1"}}, unicodeIcons(), newStyles())
	p.cursor = 1
	for _, key := range "1.21.1" {
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	}
	_, selected := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if selected == nil || selected.value != "1.21.1" {
		t.Fatalf("exact version was not selected: %+v", selected)
	}
}
