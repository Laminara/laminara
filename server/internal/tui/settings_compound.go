package tui

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/settings"
)

type settingsCompound struct {
	path          string
	kind          string
	parentFirst   int
	focus         string
	pendingRemove string
	removePending bool
}

func (m Model) openSettingsCompound(path string) (tea.Model, tea.Cmd) {
	if m.settings.page == nil {
		return m, nil
	}
	for _, entry := range m.settings.page.Entries {
		if entry.Path == path && (entry.Kind == "pairs" || entry.Kind == "map" || entry.Kind == "list") {
			m.settings.compound = &settingsCompound{path: path, kind: entry.Kind, parentFirst: m.settings.menu.first}
			m.settings.menu.cursor = 0
			m.fillSettingsCompound()
			break
		}
	}
	return m, nil
}

func (m *Model) fillSettingsCompound() {
	editor := m.settings.compound
	var field *adminv1.SettingEntry
	for _, entry := range m.settings.page.Entries {
		if entry.Path == editor.path {
			field = entry
			break
		}
	}
	if field == nil {
		m.settings.compound = nil
		return
	}
	var items []menuItem
	if editor.kind == "pairs" || editor.kind == "map" {
		values := map[string]string{}
		if field.Value != "" {
			if err := json.Unmarshal([]byte(field.Value), &values); err != nil {
				m.settings.menu.problem = "словарь не читается как JSON"
				return
			}
		}
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			kind := editLiteral
			if editor.kind == "pairs" && settings.HeaderSecret(key) {
				kind = editSecret
			}
			items = append(items, menuItem{id: "entry:" + key, label: key, value: values[key], editor: kind, hint: "Enter — заменить значение; x — удалить запись"})
		}
		label := "добавить заголовок"
		if editor.kind == "map" {
			label = "добавить запись"
		}
		items = append(items, menuItem{id: "add", label: label, editor: editText, action: true, hint: "Введите ключ. Затем откройте запись, чтобы задать значение."})
	} else {
		var values []string
		if strings.HasPrefix(field.Value, "[") {
			_ = json.Unmarshal([]byte(field.Value), &values)
		} else if field.Value != "" {
			values = strings.Split(field.Value, ", ")
		}
		for index, value := range values {
			items = append(items, menuItem{id: "entry:" + strconv.Itoa(index), label: strconv.Itoa(index + 1), value: value, editor: editLiteral})
		}
		items = append(items, menuItem{id: "add", label: "добавить элемент", editor: editLiteral, action: true, hint: "Пробелы, запятые и знаки равенства входят в значение."})
	}
	m.settings.menu.title = field.Label
	m.settings.menu.subtitle = m.settingsSubtitle(m.settings.page)
	m.settings.menu.setItems(items)
	if editor.focus != "" {
		for index, item := range items {
			if item.id == editor.focus {
				m.settings.menu.cursor = index
				break
			}
		}
		editor.focus = ""
		m.settings.menu.scroll()
	}
}

func (m Model) updateSettingsCompound(message tea.Msg) (tea.Model, tea.Cmd) {
	editor := *m.settings.compound
	m.settings.compound = &editor
	if key, ok := message.(tea.KeyMsg); ok && m.settings.menu.mode == menuBrowsing {
		if editor.removePending {
			editor.removePending = false
			m.settings.menu.notice = ""
			if key.String() == "y" {
				return m.stageSettingsItem(editor.pendingRemove, "", true)
			}
		}
		switch key.String() {
		case "esc":
			m.settings.compound = nil
			m.fillSettings(m.settings.page)
			for index, item := range m.settings.menu.items {
				if item.id == "field:"+editor.path {
					m.settings.menu.cursor = index
					break
				}
			}
			m.settings.menu.first = editor.parentFirst
			m.settings.menu.scroll()
			return m, nil
		case "x":
			item, ok := m.settings.menu.current()
			if ok && strings.HasPrefix(item.id, "entry:") {
				editor.pendingRemove = strings.TrimPrefix(item.id, "entry:")
				editor.removePending = true
				m.settings.menu.notice = "удалить этот элемент? y — удалить, другая клавиша — отменить"
			}
			return m, nil
		}
	}
	before := m.settings.menu
	menu, event := m.settings.menu.Update(message)
	m.settings.menu = menu
	if event.kind != menuSubmitted {
		return m, nil
	}
	m.settings.retryMenu = &before
	if event.id == "add" {
		if event.value == "" {
			return m, nil
		}
		if editor.kind == "pairs" || editor.kind == "map" {
			for _, item := range m.settings.menu.items {
				if item.id == "entry:"+event.value || (editor.kind == "pairs" && strings.EqualFold(item.id, "entry:"+event.value)) {
					m.settings.menu.problem = "такая запись уже есть — откройте её для изменения"
					return m, nil
				}
			}
			return m.stageSettingsItem(event.value, "", false)
		}
		return m.stageSettingsItem("", event.value, false)
	}
	return m.stageSettingsItem(strings.TrimPrefix(event.id, "entry:"), event.value, false)
}

func (m Model) stageSettingsItem(key, value string, remove bool) (tea.Model, tea.Cmd) {
	if !remove {
		focusKey := key
		if m.settings.compound.kind == "list" && key == "" {
			focusKey = strconv.Itoa(len(m.settings.menu.items) - 1)
		}
		m.settings.compound.focus = "entry:" + focusKey
	}
	return m.stageChange(&adminv1.SettingChange{Action: &adminv1.SettingChange_Item{Item: &adminv1.SetSettingItemRequest{Path: m.settings.compound.path, Key: key, Value: value, Remove: remove}}})
}
