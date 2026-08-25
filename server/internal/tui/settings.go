package tui

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
)

type settingsLevel struct {
	title      string
	section    string
	collection string
	entry      string
	cursor     int
}

type settingsState struct {
	menu           menu
	stack          []settingsLevel
	page           *adminv1.ListSettingsResponse
	pendingRestart bool
	pendingRemove  string
}

func (s settingsState) here() settingsLevel {
	if len(s.stack) == 0 {
		return settingsLevel{title: "Настройки проекта"}
	}
	return s.stack[len(s.stack)-1]
}

func fetchSettings(ctx context.Context, client adminv1connect.AdminServiceClient, level settingsLevel) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.ListSettings(ctx, connect.NewRequest(&adminv1.ListSettingsRequest{Section: level.section, Entry: level.entry}))
		if err != nil {
			return settingsMsg{err: err}
		}
		return settingsMsg{page: resp.Msg}
	}
}

func setSetting(ctx context.Context, client adminv1connect.AdminServiceClient, path, value string) tea.Cmd {
	return func() tea.Msg {
		_, err := client.SetSetting(ctx, connect.NewRequest(&adminv1.SetSettingRequest{Path: path, Value: value}))
		return settingsSavedMsg{path: path, err: err}
	}
}

func addSettingEntry(ctx context.Context, client adminv1connect.AdminServiceClient, collection, name string) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.AddSettingEntry(ctx, connect.NewRequest(&adminv1.AddSettingEntryRequest{Collection: collection, Name: name}))
		if err != nil {
			return settingsSavedMsg{path: collection, err: err}
		}
		return settingsSavedMsg{path: resp.Msg.Path, added: true}
	}
}

func removeSettingEntry(ctx context.Context, client adminv1connect.AdminServiceClient, path string) tea.Cmd {
	return func() tea.Msg {
		_, err := client.RemoveSettingEntry(ctx, connect.NewRequest(&adminv1.RemoveSettingEntryRequest{Path: path}))
		return settingsSavedMsg{path: path, err: err, removed: true}
	}
}

func restartProject(ctx context.Context, client adminv1connect.AdminServiceClient) tea.Cmd {
	return func() tea.Msg {
		_, err := client.Restart(ctx, connect.NewRequest(&adminv1.RestartRequest{}))
		return settingsSavedMsg{path: "restart", err: err, restarted: true}
	}
}

func (m Model) enterSettings() (tea.Model, tea.Cmd) {
	m.state = stateSettings
	m.settings = settingsState{menu: newMenu("Настройки проекта")}
	m.settings.menu.empty = "Тут пока пусто."
	m.settings.stack = []settingsLevel{{title: "Настройки проекта"}}
	m.settings.menu.resize(m.width, m.contentHeight()-4)
	return m, fetchSettings(m.ctx, m.client, m.settings.here())
}

func (m Model) reloadSettings() tea.Cmd {
	return fetchSettings(m.ctx, m.client, m.settings.here())
}

func editorOf(kind string) menuEditor {
	switch kind {
	case "bool":
		return editToggle
	case "choice":
		return editSelect
	case "secret":
		return editSecret
	default:
		return editText
	}
}

func (m *Model) fillSettings(page *adminv1.ListSettingsResponse) {
	m.settings.page = page
	here := m.settings.here()
	var items []menuItem

	if here.collection != "" {
		if collection := findCollection(page, here.collection); collection != nil {
			for _, entry := range collection.Entries {
				items = append(items, menuItem{id: "item:" + entry.Path, label: entry.Title, hint: collection.Hint})
			}
			add := menuItem{id: "add:" + collection.Path, label: "добавить", hint: collection.NameHint, action: true}
			if collection.Keyed {
				add.editor = editText
				add.hint = collection.NameLabel + ". " + collection.NameHint
			}
			items = append(items, add)
		}
		m.settings.menu.title = here.title
		m.settings.menu.setItems(items)
		return
	}

	for _, section := range page.Sections {
		items = append(items, menuItem{id: "section:" + section.Key, label: section.Title, hint: section.Hint})
	}
	for _, entry := range page.Entries {
		items = append(items, settingItem(entry))
	}
	for _, collection := range page.Collections {
		items = append(items, menuItem{
			id:    "coll:" + collection.Path,
			label: collection.Title,
			value: countWord(len(collection.Entries)),
			hint:  collection.Hint,
			tone:  toneMuted,
		})
	}
	m.settings.menu.title = here.title
	if here.section == "" && here.entry == "" {
		m.settings.menu.subtitle = "файл настроек: " + page.ConfigPath
	} else {
		m.settings.menu.subtitle = ""
	}
	m.settings.menu.setItems(items)
}

func settingItem(entry *adminv1.SettingEntry) menuItem {
	shown := entry.Display
	if shown == "" {
		shown = entry.Value
	}
	tone := toneGood
	if !entry.IsSet {
		tone = toneMuted
	}
	if shown == "" {
		shown = "не задано"
		tone = toneMuted
	}
	return menuItem{
		id:      "field:" + entry.Path,
		label:   entry.Label,
		value:   shown,
		hint:    entry.Hint,
		tone:    tone,
		editor:  editorOf(entry.Kind),
		options: entry.Options,
	}
}

func countWord(count int) string {
	if count == 0 {
		return "пусто"
	}
	return pluralEntries(count)
}

func pluralEntries(count int) string {
	switch {
	case count%100 >= 11 && count%100 <= 14:
		return itoa(count) + " записей"
	case count%10 == 1:
		return itoa(count) + " запись"
	case count%10 >= 2 && count%10 <= 4:
		return itoa(count) + " записи"
	default:
		return itoa(count) + " записей"
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func findCollection(page *adminv1.ListSettingsResponse, path string) *adminv1.SettingCollection {
	for _, collection := range page.Collections {
		if collection.Path == path {
			return collection
		}
	}
	return nil
}

func (m Model) currentCollection() *adminv1.SettingCollection {
	here := m.settings.here()
	if here.collection == "" || m.settings.page == nil {
		return nil
	}
	return findCollection(m.settings.page, here.collection)
}

func splitID(id string) (string, string) {
	kind, rest, _ := strings.Cut(id, ":")
	return kind, rest
}

func (m Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, event := m.settings.menu.Update(msg)
	m.settings.menu = updated
	kind, path := splitID(event.id)

	switch event.kind {
	case menuOpened:
		return m.openSettings(kind, path, event.id)
	case menuSubmitted:
		m.settings.menu.problem, m.settings.menu.notice = "", ""
		switch kind {
		case "field":
			return m, setSetting(m.ctx, m.client, path, event.value)
		case "add":
			if event.value == "" {
				return m, nil
			}
			return m, addSettingEntry(m.ctx, m.client, path, event.value)
		}
		return m, nil
	case menuPressed:
		return m.settingsKey(event.key, kind, path)
	}
	return m, nil
}

func (m Model) openSettings(kind, path, id string) (tea.Model, tea.Cmd) {
	m.settings.menu.problem, m.settings.menu.notice = "", ""
	m.settings.pendingRestart, m.settings.pendingRemove = false, ""
	here := m.settings.here()
	label := ""
	if item, ok := m.settings.menu.current(); ok {
		label = item.label
	}
	push := func(level settingsLevel) (tea.Model, tea.Cmd) {
		m.settings.stack[len(m.settings.stack)-1].cursor = m.settings.menu.cursor
		m.settings.stack = append(m.settings.stack, level)
		m.settings.menu.cursor, m.settings.menu.first = 0, 0
		return m, m.reloadSettings()
	}
	switch kind {
	case "section":
		return push(settingsLevel{title: label, section: path})
	case "coll":
		return push(settingsLevel{title: label, section: here.section, collection: path})
	case "item":
		return push(settingsLevel{title: label, entry: path})
	case "add":
		return m, addSettingEntry(m.ctx, m.client, path, "")
	}
	return m, nil
}

func (m Model) settingsKey(key, kind, path string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q":
		if len(m.settings.stack) > 1 {
			m.settings.stack = m.settings.stack[:len(m.settings.stack)-1]
			m.settings.menu.cursor = m.settings.here().cursor
			m.settings.menu.first = 0
			m.settings.menu.problem, m.settings.menu.notice = "", ""
			return m, m.reloadSettings()
		}
		m.state = stateMain
		return m, nil
	case "r":
		m.settings.pendingRestart = true
		m.settings.menu.notice = "перезапустить проект? y — перезапустить, любая другая — нет"
		return m, nil
	case "y":
		if m.settings.pendingRestart {
			m.settings.pendingRestart = false
			m.settings.menu.notice = "перезапускаю проект…"
			return m, restartProject(m.ctx, m.client)
		}
		if m.settings.pendingRemove != "" {
			target := m.settings.pendingRemove
			m.settings.pendingRemove = ""
			return m, removeSettingEntry(m.ctx, m.client, target)
		}
		return m, nil
	case "x":
		switch kind {
		case "field":
			return m, setSetting(m.ctx, m.client, path, "")
		case "item":
			m.settings.pendingRemove = path
			label := path
			if item, ok := m.settings.menu.current(); ok {
				label = item.label
			}
			m.settings.menu.notice = "удалить «" + label + "»? y — удалить, любая другая — нет"
			return m, nil
		}
		return m, nil
	}
	m.settings.pendingRestart = false
	m.settings.pendingRemove = ""
	if m.settings.menu.notice != "" && !strings.HasPrefix(m.settings.menu.notice, "записано") {
		m.settings.menu.notice = ""
	}
	return m, nil
}

func (m Model) onSettingsSaved(msg settingsSavedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.settings.menu.problem = errorText(msg.err)
		return m, nil
	}
	switch {
	case msg.restarted:
		m.settings.menu.notice = "проект перезапускается — консоль подключится сама"
		return m, nil
	case msg.added:
		m.settings.menu.notice = "запись добавлена"
	case msg.removed:
		m.settings.menu.notice = "запись удалена"
	default:
		m.settings.menu.notice = "записано · чтобы настройка заработала, нажмите r"
	}
	return m, m.reloadSettings()
}

func (m Model) settingsView() string {
	return m.centered(m.settings.menu.View(m.styles, m.icons))
}

func (m Model) settingsBar() string {
	item := func(letter, label string) string {
		return m.styles.key.Render(letter) + " " + m.styles.keyLabel.Render(label)
	}
	switch m.settings.menu.mode {
	case menuTyping:
		return m.styles.bar.Render(strings.Join([]string{item("Enter", "сохранить"), item("Esc", "отменить")}, "   "))
	case menuChoosing:
		return m.styles.bar.Render(strings.Join([]string{item("↑↓", "выбрать"), item("Enter", "принять"), item("Esc", "отменить")}, "   "))
	}
	left := []string{item("↑↓", "выбрать"), item("Enter", "открыть")}
	if current, ok := m.settings.menu.current(); ok {
		kind, _ := splitID(current.id)
		switch kind {
		case "field":
			left = []string{item("↑↓", "выбрать"), item("Enter", "изменить"), item("x", "вернуть как было")}
		case "item":
			left = []string{item("↑↓", "выбрать"), item("Enter", "открыть"), item("x", "удалить")}
		}
	}
	right := []string{item("r", "перезапустить"), item("Esc", "назад")}
	return m.styles.bar.Render(fitBar(left, right, m.width-4))
}
