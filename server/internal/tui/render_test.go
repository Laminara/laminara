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
	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
)

func TestDeleteConfirm(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	base := Model{icons: unicodeIcons(), styles: newStyles(), logCh: make(chan string, 1), input: textinput.New()}
	updated, _ := base.Update(tea.WindowSizeMsg{Width: 96, Height: 22})
	m := updated.(Model)

	m.confirm = deleteConfirm("Survival", m.icons)
	if !strings.Contains(m.View(), "удалить сборку") {
		t.Fatal("confirm prompt must render when a delete is pending")
	}
	fmt.Println("\n========== ПОДТВЕРЖДЕНИЕ УДАЛЕНИЯ ==========")
	fmt.Println(m.View())

	cancel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = cancel.(Model)
	if !m.confirm.empty() {
		t.Fatal("any non-yes key must cancel the delete")
	}

	m.confirm = deleteConfirm("Survival", m.icons)
	confirmed, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = confirmed.(Model)
	if !m.confirm.empty() {
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

func TestPlayersScreen(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	base := Model{icons: unicodeIcons(), styles: newStyles(), logCh: make(chan string, 1), input: textinput.New()}
	updated, _ := base.Update(tea.WindowSizeMsg{Width: 96, Height: 26})
	m := updated.(Model)
	entered, _ := m.enterPlayers()
	m = entered.(Model)
	m.fillPlayers(playersMsg{list: []*adminv1.BuildPlayers{
		{Build: "Creative"},
		{Build: "Modded", Address: "play.example.com:25565", Reachable: true, Online: 3, Max: 60, Names: []string{"neo", "trinity", "morpheus"}},
		{Build: "Skyblock", Address: "sky.example.com:25565", Error: "connection refused"},
	}})
	view := m.View()
	if !strings.Contains(view, "trinity") || !strings.Contains(view, "сервер не отвечает") {
		t.Fatal("players screen must list names and unreachable servers")
	}
	fmt.Println("\n========== КТО В ИГРЕ ==========")
	fmt.Println(view)

	down, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = down.(Model)
	banned, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = banned.(Model)
	if m.confirm.command != "ban trinity" {
		t.Fatalf("Enter must offer to ban the selected player, got %q", m.confirm.command)
	}
}

func TestBuildCard(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	base := Model{icons: unicodeIcons(), styles: newStyles(), logCh: make(chan string, 1), input: textinput.New()}
	updated, _ := base.Update(tea.WindowSizeMsg{Width: 96, Height: 26})
	m := updated.(Model)
	m.state = stateCard
	m.card = cardState{
		menu: newMenu(""),
		build: &adminv1.BuildInfo{
			Name:                 "Modded",
			Status:               "published",
			MinecraftVersion:     "1.21.1",
			JavaMajor:            21,
			Loader:               "fabric",
			SizeBytes:            1019 << 20,
			Files:                4083,
			ServerAddress:        "play.example.com:25565",
			HasFeatures:          true,
			Published:            []corev1.Platform{corev1.Platform_PLATFORM_WINDOWS_X64, corev1.Platform_PLATFORM_LINUX},
			PublishedAtUnixNanos: time.Now().Add(-30 * time.Hour).UnixNano(),
		},
		players: &adminv1.BuildPlayers{Build: "Modded", Address: "play.example.com:25565", Reachable: true, Online: 3, Max: 60},
	}
	m.card.menu.resize(m.width, m.contentHeight()-4)
	m.fillCard()
	view := m.View()
	for _, want := range []string{"Modded", "1.21.1", "fabric", "windows-x64, linux", "3 игрока из 60"} {
		if !strings.Contains(view, want) {
			t.Fatalf("card must show %q", want)
		}
	}
	fmt.Println("\n========== КАРТОЧКА СБОРКИ ==========")
	fmt.Println(view)
}

func TestBarKeepsRightSide(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	left := []string{"i собрать", "b сборки", "l игроки", "u обновить", "p опубликовать", "d удалить"}
	right := []string{"/ команда", "? справка", "q выход"}
	for _, width := range []int{40, 60, 80, 114, 200} {
		bar := fitBar(left, right, width)
		if !strings.Contains(bar, "q выход") {
			t.Fatalf("width %d dropped the right side: %q", width, bar)
		}
		if lipgloss.Width(bar) > width && width >= lipgloss.Width(strings.Join(right, "   ")) {
			t.Fatalf("width %d overflowed: %d", width, lipgloss.Width(bar))
		}
	}
}
