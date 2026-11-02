package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestSettingsRenderFitsTerminalAndKeepsHeight(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	for _, width := range []int{60, 96, 140} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := Model{styles: newStyles(), icons: unicodeIcons(), state: stateSettings, settings: settingsState{menu: sampleMenu(30)}}
			updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 28})
			m = updated.(Model)
			m.settings.menu.items[0].label = "Очень длинное название настройки источника"
			m.settings.menu.items[0].value = strings.Repeat("длинное значение ", 100)
			m.settings.menu.items[0].hint = strings.Repeat("Подсказка. ", 50)
			m.settings.menu.subtitle = "изменения сохранены · нужен перезапуск (r)"
			baseline := lipgloss.Height(m.View())
			for _, state := range []string{"browse", "text", "secret", "error", "confirm"} {
				frame := m
				frame.settings.menu.items = append([]menuItem(nil), m.settings.menu.items...)
				switch state {
				case "text":
					frame.settings.menu.startEditing()
				case "secret":
					frame.settings.menu.items[0].editor = editSecret
					frame.settings.menu.startEditing()
					frame.settings.menu.input.SetValue("fixture-secret")
				case "error":
					frame.settings.menu.problem = strings.Repeat("Не удалось сохранить. ", 40)
				case "confirm":
					frame.settings.menu.notice = "очистить настройку? y — очистить, любая другая — нет"
				}
				view := frame.View()
				if lipgloss.Width(view) > width || lipgloss.Height(view) > 28 || lipgloss.Height(view) != baseline {
					t.Errorf("%s: %dx%d, terminal %dx28, baseline height %d", state, lipgloss.Width(view), lipgloss.Height(view), width, baseline)
				}
				if strings.Contains(view, "fixture-secret") {
					t.Fatal("secret visible")
				}
			}
			t.Logf("terminal %dx28; frame height %d; five states measured", width, baseline)
		})
	}
}

func TestLongLabelsDoNotHideTheEditedValue(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	menu := sampleMenu(1)
	menu.resize(60, 20)
	menu.items[0].label = strings.Repeat("Название", 20)
	menu.items[0].value = strings.Repeat("a", 120) + "VISIBLE"
	menu.startEditing()
	if !strings.Contains(menu.View(newStyles(), unicodeIcons()), "VISIBLE") {
		t.Fatal("the label clipped the input at its cursor")
	}
	menu.resize(52, 20)
	if !strings.Contains(menu.View(newStyles(), unicodeIcons()), "VISIBLE") {
		t.Fatal("terminal resize clipped the input at its cursor")
	}
}
