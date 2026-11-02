package tui

import (
	"context"
	"fmt"
	"os"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
	"github.com/laminara/laminara/server/internal/control"
	"google.golang.org/protobuf/encoding/protojson"
)

type settingsRequest struct{ active bool }
type settingsPreviewMsg struct {
	request  *settingsRequest
	patch    *adminv1.SettingsPatch
	response *adminv1.PreviewSettingsResponse
	err      error
}
type settingsAppliedMsg struct{ err error }

func (m Model) settingsDirty() bool {
	return m.settings.patch != nil && len(m.settings.patch.Changes) > 0
}

func (m Model) settingsSubtitle(page *adminv1.ListSettingsResponse) string {
	state := m.settingsStateLabel(page)
	if page.ActiveRevision != "" && page.Revision != "" {
		state += " · файл " + page.Revision[:min(8, len(page.Revision))] + " / действует " + page.ActiveRevision[:min(8, len(page.ActiveRevision))]
	}
	return state
}

func (m Model) settingsStateLabel(page *adminv1.ListSettingsResponse) string {
	if m.settingsDirty() {
		return fmt.Sprintf("не сохранено: %d · s — проверить и сохранить", len(m.settings.patch.Changes))
	}
	if page.ReadOnlyReason != "" {
		return "внешний конфиг · правки можно экспортировать"
	}
	if m.settings.build != "" {
		return "настройки сборки · после сохранения нужна публикация"
	}
	if page.RestartPending {
		return settingsStatus(page)
	}
	return "изменения сохраняются вместе по s"
}

func (m Model) loadSettings(patch *adminv1.SettingsPatch) (tea.Model, tea.Cmd) {
	m.settings.busy = true
	m.settings.request = &settingsRequest{active: true}
	request, level := m.settings.request, m.settings.here()
	return m, previewSettings(m.ctx, m.client, request, patch, level, m.settings.build)
}

func previewSettings(ctx context.Context, client adminv1connect.AdminServiceClient, request *settingsRequest, patch *adminv1.SettingsPatch, level settingsLevel, build string) tea.Cmd {
	return func() tea.Msg {
		if err := control.RequireCapability(ctx, client, adminv1.AdminCapability_ADMIN_CAPABILITY_SETTINGS_PATCH); err != nil {
			return settingsPreviewMsg{request: request, patch: patch, err: err}
		}
		view := &adminv1.PreviewSettingsRequest{Patch: patch, Section: level.section, Entry: level.entry, Query: level.query}
		response, err := control.PreviewSettings(ctx, client, build, view)
		if err != nil {
			return settingsPreviewMsg{request: request, patch: patch, err: err}
		}
		return settingsPreviewMsg{request: request, patch: patch, response: response}
	}
}

func (m Model) onSettingsPreview(message settingsPreviewMsg) (tea.Model, tea.Cmd) {
	if m.state != stateSettings || message.request != m.settings.request {
		return m, nil
	}
	m.settings.busy = false
	if message.err != nil {
		if len(m.settings.loadedStack) > 0 {
			m.settings.stack = slices.Clone(m.settings.loadedStack)
		}
		if m.settings.retryMenu != nil {
			m.settings.menu = *m.settings.retryMenu
		}
		m.settings.retryMenu = nil
		m.settings.menu.problem = errorText(message.err)
		return m, nil
	}
	m.settings.patch = message.patch
	m.settings.loadedStack = slices.Clone(m.settings.stack)
	m.settings.retryMenu = nil
	if m.settings.patch == nil {
		m.settings.patch = &adminv1.SettingsPatch{Revision: message.response.Page.Revision, TargetBuild: m.settings.build}
	}
	m.settings.differences, m.settings.validation = message.response.Differences, message.response.ValidationError
	m.settings.menu.problem, m.settings.menu.notice = "", ""
	m.fillSettings(message.response.Page)
	if m.settings.compound != nil {
		m.fillSettingsCompound()
	}
	return m, nil
}

func (m Model) stageSetting(path, value string, mode adminv1.SettingWriteMode) (tea.Model, tea.Cmd) {
	return m.stageChange(&adminv1.SettingChange{Action: &adminv1.SettingChange_Set{Set: &adminv1.SetSettingRequest{Path: path, Value: value, Mode: mode}}})
}

func (m Model) stageChange(change *adminv1.SettingChange) (tea.Model, tea.Cmd) {
	if m.settings.patch == nil {
		m.settings.menu.problem = "дождитесь загрузки настроек"
		return m, nil
	}
	patch := &adminv1.SettingsPatch{Revision: m.settings.patch.Revision, TargetBuild: m.settings.build, Changes: append(slices.Clone(m.settings.patch.Changes), change)}
	return m.loadSettings(patch)
}

func (m Model) reviewSettings() (tea.Model, tea.Cmd) {
	if !m.settingsDirty() {
		m.settings.menu.notice = "несохранённых изменений нет"
		return m, nil
	}
	m.settings.editMenu = m.settings.menu
	m.settings.menu = newMenu("Проверка изменений")
	m.settings.review = true
	m.settings.menu.resize(m.width, m.contentHeight()-4)
	for _, difference := range m.settings.differences {
		m.settings.menu.items = append(m.settings.menu.items, menuItem{id: difference.Path, label: difference.Path, value: difference.After, hint: "было: " + difference.Before + " · станет: " + difference.After})
	}
	m.settings.menu.items = append(m.settings.menu.items, menuItem{id: "export", label: "экспортировать правки", value: "settings.patch.json", hint: "Файл на машине, где запущена эта консоль. Новые введённые секреты входят в файл; права будут 0600.", action: true, editor: editText})
	m.settings.menu.problem = m.settings.validation
	if m.settings.page.ReadOnlyReason != "" {
		m.settings.menu.notice = m.settings.page.ReadOnlyReason
	}
	return m, nil
}

func (m Model) updateSettingsReview(message tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := message.(tea.KeyMsg); ok && m.settings.menu.mode == menuBrowsing {
		switch key.String() {
		case "esc":
			m.settings.review = false
			m.settings.menu = m.settings.editMenu
			return m, nil
		case "e":
			m.settings.menu.cursor = len(m.settings.menu.items) - 1
			m.settings.menu.startEditing()
			return m, nil
		case "s":
			if m.settings.validation != "" {
				m.settings.menu.problem = m.settings.validation
				return m, nil
			}
			if m.settings.page.ReadOnlyReason != "" {
				m.settings.menu.problem = m.settings.page.ReadOnlyReason
				return m, nil
			}
			m.settings.busy = true
			patch, client, ctx := m.settings.patch, m.client, m.ctx
			return m, func() tea.Msg {
				_, err := control.ApplySettings(ctx, client, patch)
				return settingsAppliedMsg{err: err}
			}
		}
	}
	menu, event := m.settings.menu.Update(message)
	m.settings.menu = menu
	if event.kind == menuSubmitted && event.id == "export" {
		if err := exportSettingsPatch(event.value, m.settings.patch); err != nil {
			m.settings.menu.problem = err.Error()
		} else {
			m.settings.menu.notice = "правки экспортированы: " + event.value
		}
	}
	return m, nil
}

func exportSettingsPatch(path string, patch *adminv1.SettingsPatch) error {
	data, err := (protojson.MarshalOptions{Indent: "  "}).Marshal(patch)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("создать файл правок: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		os.Remove(path)
		return err
	}
	return file.Close()
}

func (m Model) onSettingsApplied(message settingsAppliedMsg) (tea.Model, tea.Cmd) {
	m.settings.busy = false
	if message.err != nil {
		m.settings.menu.problem = errorText(message.err)
		return m, nil
	}
	m.settings.review, m.settings.patch = false, nil
	m.settings.menu = m.settings.editMenu
	updated, load := m.loadSettings(nil)
	if m.settings.build != "" {
		return updated, tea.Batch(load, fetchBuilds(m.ctx, m.client))
	}
	return updated, load
}

func (m Model) enterBuildSettings(name string) (tea.Model, tea.Cmd) {
	m.state = stateSettings
	m.settings = settingsState{build: name, menu: newMenu("Настройки «" + name + "»"), stack: []settingsLevel{{title: "Настройки «" + name + "»", section: "profile"}}}
	m.settings.menu.resize(m.width, m.contentHeight()-4)
	return m.loadSettings(nil)
}
