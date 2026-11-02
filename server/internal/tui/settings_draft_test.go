package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
)

func TestSettingsFailedPreviewRestoresInputAndNavigation(t *testing.T) {
	m := Model{state: stateSettings, settings: settingsState{menu: sampleMenu(1), patch: &adminv1.SettingsPatch{Revision: "old"}, stack: []settingsLevel{{section: "auth"}}, loadedStack: []settingsLevel{{section: "auth"}}}}
	m.settings.menu.items[0] = menuItem{id: "field:auth.config.dsn", label: "DSN", editor: editSecret}
	updated, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	m.settings.menu.input.SetValue("kept-secret")
	updated, _ = m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.onSettingsPreview(settingsPreviewMsg{request: m.settings.request, err: errors.New("unavailable")})
	m = updated.(Model)
	if m.settings.menu.mode != menuTyping || m.settings.menu.input.Value() != "kept-secret" {
		t.Fatal("failed edit discarded typed input")
	}
	if strings.Contains(m.settings.menu.View(newStyles(), unicodeIcons()), "kept-secret") {
		t.Fatal("restored secret is visible")
	}
	if m.settingsDirty() {
		t.Fatal("failed change entered the patch")
	}
	m.settings.stack = append(m.settings.stack, settingsLevel{section: "storage"})
	updated, _ = m.loadSettings(m.settings.patch)
	m = updated.(Model)
	updated, _ = m.onSettingsPreview(settingsPreviewMsg{request: m.settings.request, err: errors.New("unavailable")})
	m = updated.(Model)
	if m.settings.here().section != "auth" {
		t.Fatal("failed navigation changed the active page")
	}
}

func TestSettingsExportNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "patch.json")
	patch := &adminv1.SettingsPatch{Revision: "original"}
	if err := exportSettingsPatch(path, patch); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := exportSettingsPatch(path, &adminv1.SettingsPatch{Revision: "new"}); err == nil {
		t.Fatal("export overwrote existing file")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("existing export changed")
	}
}

func TestNewEditorGeometry(t *testing.T) {
	for _, width := range []int{60, 96, 140} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := newMenu("JSON")
			m.resize(width, 18)
			m.setItems([]menuItem{{id: "json", label: strings.Repeat("параметр ", 20), value: "{\"key\":\"value\"}", editor: editJSON}})
			m.startEditing()
			m.problem = strings.Repeat("ошибка ", 30)
			view := m.View(newStyles(), unicodeIcons())
			if lipgloss.Width(view) > width || lipgloss.Height(view) > 18 {
				t.Fatalf("JSON %dx%d in %dx18", lipgloss.Width(view), lipgloss.Height(view), width)
			}
			for _, step := range []wizardStep{wzPlatforms, wzReview} {
				w := wizard{icons: unicodeIcons(), styles: newStyles(), step: step, mc: "1.21.1", platforms: []string{"windows-x64", "linux"}, platformMenu: newMenu("Платформы")}
				w.fillPlatforms()
				view := w.View(width, 22)
				if lipgloss.Width(view) > width || lipgloss.Height(view) > 22 {
					t.Fatalf("wizard %d: %dx%d in %dx22", step, lipgloss.Width(view), lipgloss.Height(view), width)
				}
			}
		})
	}
}

func TestBuildRefreshKeepsSettingsScreenAndUpdatesCard(t *testing.T) {
	m := Model{state: stateSettings, styles: newStyles(), icons: unicodeIcons()}
	m.card.build = &adminv1.BuildInfo{Name: "pack", Status: "published"}
	updated, _ := m.onBuilds(buildsMsg{builds: []*adminv1.BuildInfo{{Name: "pack", Status: "changed"}}})
	m = updated.(Model)
	if m.state != stateSettings || m.card.build.Status != "changed" {
		t.Fatal("refresh changed screen or left stale card")
	}
}
