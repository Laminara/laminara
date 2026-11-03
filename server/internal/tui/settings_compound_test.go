package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
)

func compoundSettingsModel(kind, value string) Model {
	m := Model{ctx: context.Background(), client: &settingsRecorder{}, state: stateSettings}
	m.settings = settingsState{
		menu:  newMenu("Модуль"),
		stack: []settingsLevel{{section: "modules", entry: "modules.config.demo"}},
		patch: &adminv1.SettingsPatch{Revision: "initial"},
	}
	m.settings.menu.resize(100, 30)
	m.fillSettings(&adminv1.ListSettingsResponse{Entries: []*adminv1.SettingEntry{
		{Path: "modules.config.demo.token", Label: "Токен", Kind: "secret", Group: "Подключение"},
		{Path: "modules.config.demo.values", Label: "Значения", Kind: kind, Value: value},
	}})
	m.settings.menu.cursor = 2
	opened, _ := m.openSettingsCompound("modules.config.demo.values")
	return opened.(Model)
}

func TestCompoundPreviewKeepsSelection(t *testing.T) {
	m := compoundSettingsModel("list", `["first","second"]`)
	staged, _ := m.stageSettingsItem("0", "changed", false)
	m = staged.(Model)
	updated, _ := m.onSettingsPreview(settingsPreviewMsg{
		request:  m.settings.request,
		patch:    m.settings.patch,
		response: &adminv1.PreviewSettingsResponse{Page: m.settings.page},
	})
	m = updated.(Model)
	item, _ := m.settings.menu.current()
	if item.id != "entry:0" {
		t.Fatalf("editing first item moved focus to %q", item.id)
	}
	closed, _ := m.updateSettingsCompound(tea.KeyMsg{Type: tea.KeyEsc})
	m = closed.(Model)
	item, _ = m.settings.menu.current()
	if item.id != "field:modules.config.demo.values" {
		t.Fatalf("return lost original field: %q", item.id)
	}
}

func TestCompoundAddFocusesNewMapEntry(t *testing.T) {
	m := compoundSettingsModel("map", `{"Region":"eu","region":"test"}`)
	m.settings.menu.cursor = len(m.settings.menu.items) - 1
	staged, _ := m.stageSettingsItem("REGION", "", false)
	m = staged.(Model)
	m.settings.page.Entries[1].Value = `{"REGION":"","Region":"eu","region":"test"}`
	updated, _ := m.onSettingsPreview(settingsPreviewMsg{
		request:  m.settings.request,
		patch:    m.settings.patch,
		response: &adminv1.PreviewSettingsResponse{Page: m.settings.page},
	})
	item, _ := updated.(Model).settings.menu.current()
	if item.id != "entry:REGION" {
		t.Fatalf("new entry not selected: %q", item.id)
	}
}
