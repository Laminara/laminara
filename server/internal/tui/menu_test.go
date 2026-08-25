package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func sampleMenu(count int) menu {
	items := make([]menuItem, 0, count)
	for index := 0; index < count; index++ {
		items = append(items, menuItem{
			id:     "field:поле" + itoa(index),
			label:  "Настройка " + itoa(index),
			value:  "значение " + itoa(index),
			hint:   "Что делает настройка " + itoa(index) + ".",
			editor: editText,
		})
	}
	menu := newMenu("Раздел")
	menu.resize(120, 20)
	menu.setItems(items)
	return menu
}

func press(m menu, key tea.KeyMsg) (menu, menuEvent) {
	return m.Update(key)
}

func TestMenuKeepsItsPlaceWhileEditing(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := sampleMenu(30)
	for step := 0; step < 20; step++ {
		m, _ = press(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	before := m.first
	beforeView := m.View(newStyles(), unicodeIcons())

	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != menuTyping {
		t.Fatal("Enter on a text field must start editing")
	}
	if m.first != before {
		t.Fatalf("the list moved while editing: %d → %d", before, m.first)
	}
	afterView := m.View(newStyles(), unicodeIcons())
	if lipgloss.Height(beforeView) != lipgloss.Height(afterView) {
		t.Fatalf("the box changed height: %d → %d", lipgloss.Height(beforeView), lipgloss.Height(afterView))
	}
	if !strings.Contains(afterView, "Настройка 20") {
		t.Fatal("the edited row must stay visible")
	}
}

func TestMenuViewHeightIsStable(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	short := sampleMenu(3)
	long := sampleMenu(40)
	if lipgloss.Height(short.View(newStyles(), unicodeIcons())) != lipgloss.Height(long.View(newStyles(), unicodeIcons())) {
		t.Fatal("a short menu and a long one must draw the same box")
	}

	choosing := sampleMenu(30)
	choosing.items[0] = menuItem{id: "field:режим", label: "Режим", value: "observe", editor: editSelect, options: []string{"off", "observe", "enforce"}}
	choosing.cursor = 0
	browsing := lipgloss.Height(choosing.View(newStyles(), unicodeIcons()))
	choosing, _ = press(choosing, tea.KeyMsg{Type: tea.KeyEnter})
	if choosing.mode != menuChoosing {
		t.Fatal("Enter on a choice must open the options")
	}
	if opened := lipgloss.Height(choosing.View(newStyles(), unicodeIcons())); opened != browsing {
		t.Fatalf("opening the options changed the box: %d → %d", browsing, opened)
	}
}

func TestMenuSubmitsEditedValue(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := sampleMenu(5)
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.input.SetValue("новое")
	m, event := press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if event.kind != menuSubmitted || event.id != "field:поле0" || event.value != "новое" {
		t.Fatalf("event = %+v", event)
	}
	if m.mode != menuBrowsing {
		t.Fatal("saving must close the editor")
	}
}

func TestMenuSkipsStaticRows(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := newMenu("Список")
	m.resize(120, 20)
	m.setItems([]menuItem{
		{id: "head", label: "Сборка", static: true},
		{id: "player:neo", label: "neo"},
		{id: "head2", label: "Другая сборка", static: true},
		{id: "player:trinity", label: "trinity"},
	})
	if current, _ := m.current(); current.id != "player:neo" {
		t.Fatalf("the cursor must land on a selectable row, got %q", current.id)
	}
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyDown})
	if current, _ := m.current(); current.id != "player:trinity" {
		t.Fatalf("headers must be skipped, got %q", current.id)
	}
}

func TestMenuBubblesUnknownKeys(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	m := sampleMenu(3)
	_, event := press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if event.kind != menuPressed || event.key != "r" {
		t.Fatalf("unhandled keys must reach the screen: %+v", event)
	}
}
