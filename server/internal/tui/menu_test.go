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

func TestMenuLongMetadataKeepsFrameHeight(t *testing.T) {
	m := sampleMenu(2)
	m.resize(58, 20)
	before := lipgloss.Height(m.View(newStyles(), unicodeIcons()))
	m.title = strings.Repeat("Модуль ", 40)
	m.subtitle = strings.Repeat("Состояние ", 40)
	view := m.View(newStyles(), unicodeIcons())
	if lipgloss.Height(view) != before || lipgloss.Width(view) > m.width {
		t.Fatalf("metadata expanded frame: before=%d after=%dx%d", before, lipgloss.Width(view), lipgloss.Height(view))
	}
}

func TestMenuLongChoiceListStaysInsideFrame(t *testing.T) {
	m := sampleMenu(2)
	m.resize(58, 20)
	for i := range 40 {
		m.items[0].options = append(m.items[0].options, "Вариант "+itoa(i))
	}
	m.items[0].editor = editSelect
	m.startEditing()
	m.choice = 39
	view := m.View(newStyles(), unicodeIcons())
	if lipgloss.Height(view) > m.height || !strings.Contains(view, "Вариант 39") {
		t.Fatalf("choice list escaped frame or hid selection: height=%d limit=%d", lipgloss.Height(view), m.height)
	}
}

func TestMenuCancelClearsAbandonedValidationError(t *testing.T) {
	for _, test := range []struct {
		name string
		mode menuMode
	}{{"text", menuTyping}, {"json", menuMultiline}, {"choice", menuChoosing}} {
		t.Run(test.name, func(t *testing.T) {
			m := sampleMenu(2)
			if test.mode == menuMultiline {
				m.items[0].editor = editJSON
			}
			m.startEditing()
			m.mode = test.mode
			m.problem = "Значение не соответствует схеме"
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if m.mode != menuBrowsing || m.problem != "" {
				t.Fatalf("cancel kept stale error: %q", m.problem)
			}
		})
	}
}

func press(m menu, key tea.KeyMsg) (menu, menuEvent) {
	return m.Update(key)
}

func TestLiteralEditorOpensAndPreservesWhitespace(t *testing.T) {
	for _, action := range []bool{false, true} {
		m := newMenu("Список")
		m.setItems([]menuItem{{id: "item", label: "Элемент", editor: editLiteral, action: action}})
		m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnter})
		if m.mode != menuTyping {
			t.Fatalf("literal editor did not open (action=%v)", action)
		}
		value := "  a,b=c  "
		m, _ = press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)})
		_, event := press(m, tea.KeyMsg{Type: tea.KeyEnter})
		if event.kind != menuSubmitted || event.value != value {
			t.Fatalf("literal value changed: %#v", event)
		}
	}
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

func TestMenuSecretInputDoesNotRevealOrClearExistingValue(t *testing.T) {
	m := sampleMenu(2)
	m.items[0].editor = editSecret
	m.items[0].value = "***"
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, event := press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if event.kind != menuNothing {
		t.Fatal("empty secret edit must not submit a change")
	}
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.input.SetValue("fixture-secret")
	if strings.Contains(m.View(newStyles(), unicodeIcons()), "fixture-secret") {
		t.Fatal("typed secret visible")
	}
	m, event = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if event.value != "fixture-secret" || m.input.Value() != "" {
		t.Fatal("secret must be submitted then removed from input buffer")
	}
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	m.input.SetValue("public-value")
	if !strings.Contains(m.View(newStyles(), unicodeIcons()), "public-value") {
		t.Fatal("ordinary field remained masked after editing a secret")
	}
}

func TestMenuKeepsLongValues(t *testing.T) {
	m := sampleMenu(1)
	value := strings.Repeat("длинное значение, ", 300)
	m.items[0].value = value
	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEnter})
	_, event := press(m, tea.KeyMsg{Type: tea.KeyEnter})
	if event.value != strings.TrimSpace(value) {
		t.Fatalf("long value truncated: got %d bytes, want %d", len(event.value), len(value))
	}
}
