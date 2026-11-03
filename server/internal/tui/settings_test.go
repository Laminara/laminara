package tui

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
)

type settingsRecorder struct {
	adminv1connect.AdminServiceClient
	writes  []*adminv1.SetSettingRequest
	patches []*adminv1.SettingsPatch
}

func TestSettingsNarrowBarKeepsEnter(t *testing.T) {
	m := Model{width: 58, styles: newStyles(), settings: settingsState{menu: sampleMenu(1)}}
	if !strings.Contains(m.settingsBar(), "Enter") {
		t.Fatal("primary action missing from narrow settings footer")
	}
}

func TestModuleSettingsUseSharedNavigationAndFieldEditors(t *testing.T) {
	m := Model{ctx: context.Background(), client: &settingsRecorder{}, width: 100, height: 32}
	entered, _ := m.enterSettingsAt(true)
	m = entered.(Model)
	m.fillSettings(&adminv1.ListSettingsResponse{Collections: []*adminv1.SettingCollection{{Path: "modules.config", Keyed: true, Entries: []*adminv1.SettingCollectionEntry{{Path: "modules.config.demo%2Eplugin", Title: "Демо", Hint: "Требует настройки"}}}}})
	if m.settings.menu.items[0].id != "item:modules.config.demo%2Eplugin" || m.settings.menu.items[0].hint != "Требует настройки" {
		t.Fatalf("module item: %+v", m.settings.menu.items[0])
	}
	opened, _ := m.openSettings("item", "modules.config.demo%2Eplugin", m.settings.menu.items[0].id)
	m = opened.(Model)
	min := int64(1)
	m.fillSettings(&adminv1.ListSettingsResponse{Entries: []*adminv1.SettingEntry{
		{Path: "modules.config.demo%2Eplugin.token", Label: "Токен", Group: "Подключение", Kind: "secret", Required: true},
		{Path: "modules.config.demo%2Eplugin.workers", Label: "Потоки", Group: "Подключение", Kind: "int", Min: &min},
		{Path: "modules.config.demo%2Eplugin.labels", Label: "Метки", Kind: "map"},
	}})
	items := m.settings.menu.items
	if len(items) != 4 || !items[0].static || items[1].editor != editSecret || !strings.Contains(items[2].hint, "Минимум: 1") || items[3].editor != editNone {
		t.Fatalf("fields: %+v", items)
	}
	if m.settings.here().entry != "modules.config.demo%2Eplugin" {
		t.Fatal("entry navigation lost")
	}
}

func (s *settingsRecorder) Status(context.Context, *connect.Request[adminv1.StatusRequest]) (*connect.Response[adminv1.StatusResponse], error) {
	return connect.NewResponse(&adminv1.StatusResponse{Capabilities: []adminv1.AdminCapability{adminv1.AdminCapability_ADMIN_CAPABILITY_REDACTED_SETTINGS, adminv1.AdminCapability_ADMIN_CAPABILITY_SETTINGS_PATCH}}), nil
}

func (s *settingsRecorder) PreviewSettings(_ context.Context, req *connect.Request[adminv1.PreviewSettingsRequest]) (*connect.Response[adminv1.PreviewSettingsResponse], error) {
	s.patches = append(s.patches, req.Msg.Patch)
	return connect.NewResponse(&adminv1.PreviewSettingsResponse{Page: &adminv1.ListSettingsResponse{Revision: "original", Entries: []*adminv1.SettingEntry{{Path: "auth.config.dsn", Label: "Пароль", Kind: "secret", Value: "***", IsSet: true}}}}), nil
}

func (s *settingsRecorder) SetSetting(_ context.Context, req *connect.Request[adminv1.SetSettingRequest]) (*connect.Response[adminv1.SetSettingResponse], error) {
	s.writes = append(s.writes, req.Msg)
	return connect.NewResponse(&adminv1.SetSettingResponse{}), nil
}

func TestSettingsConfirmationsCancelOnNavigation(t *testing.T) {
	for _, action := range []string{"remove", "restart", "clear"} {
		t.Run(action, func(t *testing.T) {
			m := Model{settings: settingsState{menu: sampleMenu(2)}}
			switch action {
			case "remove":
				m.settings.pendingRemove = "access.rules.0"
			case "restart":
				m.settings.pendingRestart = true
			case "clear":
				m.settings.pendingClear = "console.password"
			}
			updated, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyDown})
			m = updated.(Model)
			if m.settings.pendingRemove != "" || m.settings.pendingRestart || m.settings.pendingClear != "" {
				t.Fatal("navigation kept a stale confirmation")
			}
			_, cmd := m.updateSettings(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
			if cmd != nil {
				t.Fatal("cancelled action still executes")
			}
		})
	}
}

func TestSettingsSecretReplaceAndClearUseExplicitModes(t *testing.T) {
	client := &settingsRecorder{}
	m := Model{ctx: context.Background(), client: client, state: stateSettings, settings: settingsState{menu: sampleMenu(1), stack: []settingsLevel{{section: "auth"}}, patch: &adminv1.SettingsPatch{Revision: "original"}}}
	m.settings.menu.items[0] = menuItem{id: "field:auth.config.dsn", label: "Пароль", value: "***", editor: editSecret}
	updated, _ := m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	m.settings.menu.input.SetValue("***")
	updated, cmd := m.updateSettings(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("replacement not submitted")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if len(client.writes) != 0 || len(client.patches) != 1 || client.patches[0].Changes[0].GetSet().Mode != adminv1.SettingWriteMode_SETTING_WRITE_MODE_REPLACE || client.patches[0].Changes[0].GetSet().Value != "***" {
		t.Fatal("literal secret replacement changed")
	}
	updated, cmd = m.updateSettings(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updated.(Model)
	if cmd != nil || m.settings.pendingClear != "auth.config.dsn" {
		t.Fatal("secret clear must ask for confirmation")
	}
	_, cmd = m.updateSettings(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("confirmed clear not submitted")
	}
	cmd()
	if len(client.writes) != 0 || client.patches[1].Changes[1].GetSet().Mode != adminv1.SettingWriteMode_SETTING_WRITE_MODE_CLEAR {
		t.Fatal("clear not explicit")
	}
}

func TestSettingsPendingRestartSurvivesNavigation(t *testing.T) {
	m := Model{settings: settingsState{menu: sampleMenu(1)}}
	m.settings.stack = []settingsLevel{{title: "Настройки"}, {title: "Аккаунты", section: "auth"}}
	m.fillSettings(&adminv1.ListSettingsResponse{RestartPending: true})
	if !strings.Contains(m.settings.menu.subtitle, "нужен перезапуск") {
		t.Fatal("saved but inactive settings not marked")
	}
}
