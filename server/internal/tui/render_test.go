package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
)

func TestDeleteConfirm(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	base := Model{icons: unicodeIcons(), styles: newStyles(), logCh: make(chan string, 1), input: textinput.New()}
	updated, _ := base.Update(tea.WindowSizeMsg{Width: 96, Height: 22})
	m := updated.(Model)

	m.confirmDelete = "Survival"
	if !strings.Contains(m.View(), "удалить сборку") {
		t.Fatal("confirm prompt must render when a delete is pending")
	}
	fmt.Println("\n========== ПОДТВЕРЖДЕНИЕ УДАЛЕНИЯ ==========")
	fmt.Println(m.View())

	cancel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = cancel.(Model)
	if m.confirmDelete != "" {
		t.Fatal("any non-yes key must cancel the delete")
	}

	m.confirmDelete = "Survival"
	confirmed, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = confirmed.(Model)
	if m.confirmDelete != "" {
		t.Fatal("y must clear the pending delete")
	}
	if cmd == nil {
		t.Fatal("y must dispatch the delete command")
	}
}

func TestCommandInput(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	input := textinput.New()
	input.Prompt = "› "
	base := Model{icons: unicodeIcons(), styles: newStyles(), logCh: make(chan string, 1), input: input}
	updated, _ := base.Update(tea.WindowSizeMsg{Width: 96, Height: 22})
	m := updated.(Model)

	step, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = step.(Model)
	if !m.commandMode {
		t.Fatal("pressing / must enter command mode")
	}

	for _, r := range "versions 1.21" {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	if m.input.Value() != "versions 1.21" {
		t.Fatalf("typed command not captured: %q", m.input.Value())
	}
	if !strings.Contains(m.View(), "versions 1.21") {
		t.Fatal("command input not rendered in the bar")
	}

	out, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = out.(Model)
	if m.commandMode {
		t.Fatal("esc must leave command mode")
	}

	fmt.Println("\n========== РЕЖИМ КОМАНДЫ ==========")
	entered, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = entered.(Model)
	for _, r := range "publish Survival" {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	fmt.Println(m.View())
}

func TestRenderFrames(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)

	base := Model{icons: unicodeIcons(), styles: newStyles(), logCh: make(chan string, 1)}
	updated, _ := base.Update(tea.WindowSizeMsg{Width: 96, Height: 22})
	m := updated.(Model)
	now := time.Now().UnixNano()
	m.appendLog(formatLog(&adminv1.LogLine{TimeUnixNanos: now, Source: "build", Message: "building survival (mc=1.21.1 loader=neoforge/21.1.235)"}, m.styles))
	m.appendLog(formatLog(&adminv1.LogLine{TimeUnixNanos: now, Source: "build", Message: "downloaded 117 libraries"}, m.styles))
	m.appendLog(m.styles.dim.Render("▸ install survival 1.21.1 loader=neoforge"))

	fmt.Println("\n========== MAIN ==========")
	fmt.Println(m.View())

	m.running = true
	m.runLabel = "install survival 1.21.1 loader=neoforge"
	m.started = time.Now().Add(-12 * time.Second)
	m.spinFrame = 2
	m.prog = progressMsg{phase: "Клиент и библиотеки", current: 45, total: 117}
	fmt.Println("\n========== MAIN + ПРОГРЕСС ==========")
	fmt.Println(m.View())
	m.running = false

	items := []pickItem{
		{label: "1.21.1", value: "1.21.1", hint: "release"},
		{label: "1.21", value: "1.21", hint: "release"},
		{label: "1.20.6", value: "1.20.6", hint: "release"},
		{label: "24w14a", value: "24w14a", hint: "snapshot"},
	}
	wiz := wizard{icons: unicodeIcons(), styles: newStyles(), step: wzVersion}
	wiz.pick = newPicker(wiz.icons.install+" Версия Minecraft", items, wiz.icons, wiz.styles)
	m.wizard = wiz
	m.state = stateWizard
	fmt.Println("\n========== МАСТЕР: выбор версии ==========")
	fmt.Println(m.View())

	name := textinput.New()
	name.Prompt = ""
	name.SetValue("Survival")
	name.Focus()
	m.wizard = wizard{icons: unicodeIcons(), styles: newStyles(), step: wzName, mc: "1.21.1", loader: "neoforge", loaderVersion: "21.1.235", name: name}
	fmt.Println("\n========== МАСТЕР: имя сборки ==========")
	fmt.Println(m.View())
}

func TestComplete(t *testing.T) {
	commands := []string{"builds", "ban", "bans", "install", "publish"}
	cases := []struct {
		typed  string
		want   string
		wantOk bool
	}{
		{"pub", "publish ", true},
		{"b", "b", false},
		{"ba", "ban", true},
		{"buil", "builds ", true},
		{"zzz", "", false},
		{"publish Sur", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := complete(c.typed, commands)
		if ok != c.wantOk || (ok && got != c.want) {
			t.Fatalf("complete(%q) = %q,%v; want %q,%v", c.typed, got, ok, c.want, c.wantOk)
		}
	}
}
