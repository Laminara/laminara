package tui

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
	"github.com/laminara/laminara/server/internal/control"
)

type settingsLevel struct {
	title      string
	section    string
	collection string
	entry      string
	cursor     int
	query      string
}

type settingsState struct {
	build                string
	menu                 menu
	stack                []settingsLevel
	page                 *adminv1.ListSettingsResponse
	pendingRestart       bool
	pendingRemove        string
	pendingClear         string
	patch                *adminv1.SettingsPatch
	request              *settingsRequest
	busy                 bool
	review               bool
	editMenu             menu
	differences          []*adminv1.SettingDifference
	validation           string
	pendingDiscard       bool
	leaveAfterDiscard    bool
	compound             *settingsCompound
	passwordConfirmation string
	loadedStack          []settingsLevel
	retryMenu            *menu
}

func (s settingsState) here() settingsLevel {
	if len(s.stack) == 0 {
		return settingsLevel{title: "Настройки проекта"}
	}
	return s.stack[len(s.stack)-1]
}

func fetchSettings(ctx context.Context, client adminv1connect.AdminServiceClient, level settingsLevel) tea.Cmd {
	return func() tea.Msg {
		if err := control.RequireCapability(ctx, client, adminv1.AdminCapability_ADMIN_CAPABILITY_REDACTED_SETTINGS); err != nil {
			return settingsMsg{err: err}
		}
		resp, err := client.ListSettings(ctx, connect.NewRequest(&adminv1.ListSettingsRequest{Section: level.section, Entry: level.entry}))
		if err != nil {
			return settingsMsg{err: err}
		}
		return settingsMsg{page: resp.Msg}
	}
}

func setSetting(ctx context.Context, client adminv1connect.AdminServiceClient, path, value string, mode adminv1.SettingWriteMode) tea.Cmd {
	return func() tea.Msg {
		if err := control.RequireCapability(ctx, client, adminv1.AdminCapability_ADMIN_CAPABILITY_REDACTED_SETTINGS); err != nil {
			return settingsSavedMsg{path: path, err: err}
		}
		_, err := client.SetSetting(ctx, connect.NewRequest(&adminv1.SetSettingRequest{Path: path, Value: value, Mode: mode}))
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
	return m.enterSettingsAt(false)
}

func (m Model) enterSettingsAt(modules bool) (tea.Model, tea.Cmd) {
	m.state = stateSettings
	m.settings = settingsState{menu: newMenu("Настройки проекта")}
	m.settings.menu.empty = "Тут пока пусто."
	m.settings.stack = []settingsLevel{{title: "Настройки проекта"}}
	if modules {
		m.settings.stack = append(m.settings.stack, settingsLevel{title: "Модули", section: "modules"}, settingsLevel{title: "Настройки модулей", section: "modules", collection: "modules.config"})
	}
	m.settings.menu.resize(m.width, m.contentHeight()-4)
	return m.loadSettings(nil)
}

func (m Model) reloadSettings() tea.Cmd {
	return fetchSettings(m.ctx, m.client, m.settings.here())
}

func editorOf(kind string) menuEditor {
	switch kind {
	case "pairs", "map", "list":
		return editNone
	case "json":
		return editJSON
	case "bool":
		return editToggle
	case "choice":
		return editSelect
	case "secret", "secret-json":
		return editSecret
	default:
		return editText
	}
}

func (m *Model) fillSettings(page *adminv1.ListSettingsResponse) {
	m.settings.page = page
	m.settings.pendingRestart, m.settings.pendingRemove, m.settings.pendingClear = false, "", ""
	here := m.settings.here()
	var items []menuItem
	if here.section == "" && here.entry == "" && here.collection == "" && here.query == "" {
		items = append(items, menuItem{id: "search:", label: "найти настройку", hint: "Русское название или технический путь, например «пароль» или «auth.config»", editor: editText, action: true})
	}

	if here.collection != "" {
		if collection := findCollection(page, here.collection); collection != nil {
			if collection.Path == "modules.config" && len(collection.Entries) == 0 {
				items = append(items, menuItem{id: "empty:modules", label: "Модули не обнаружены", static: true, tone: toneMuted})
			}
			for _, entry := range collection.Entries {
				hint := collection.Hint
				if entry.Hint != "" {
					hint = entry.Hint
				}
				items = append(items, menuItem{id: "item:" + entry.Path, label: entry.Title, hint: hint})
			}
			add := menuItem{id: "add:" + collection.Path, label: "добавить", hint: collection.NameHint, action: true}
			if collection.Keyed {
				add.editor = editText
				add.hint = collection.NameLabel + ". " + collection.NameHint
			}
			if collection.Path == "modules.config" {
				add.label = "настроить по имени файла"
				add.hint = "Создаёт только настройки. Поместите исполняемый файл в каталог модулей и перезапустите сервер. " + collection.NameHint
			}
			items = append(items, add)
		}
		m.settings.menu.title = here.title
		m.settings.menu.subtitle = settingsStatus(page)
		m.settings.menu.subtitle = m.settingsSubtitle(page)
		m.settings.menu.setItems(items)
		return
	}

	for _, section := range page.Sections {
		items = append(items, menuItem{id: "section:" + section.Key, label: section.Title, hint: section.Hint})
	}
	group := ""
	for _, entry := range page.Entries {
		if entry.Group != group && entry.Group != "" && here.query == "" {
			items = append(items, menuItem{id: "group:" + entry.Path, label: entry.Group, static: true, tone: toneMuted})
		}
		group = entry.Group
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
	if page.RestartPending {
		m.settings.menu.subtitle = settingsStatus(page)
	}
	m.settings.menu.subtitle = m.settingsSubtitle(page)
	m.settings.menu.setItems(items)
}

func settingsStatus(page *adminv1.ListSettingsResponse) string {
	if page.RestartPending {
		return "изменения сохранены · нужен перезапуск (r)"
	}
	return ""
}

func settingItem(entry *adminv1.SettingEntry) menuItem {
	stored := entry.Value
	shown := entry.Display
	if shown == "" {
		shown = entry.Value
	}
	hint := entry.Hint
	if entry.Required {
		hint = strings.TrimSpace(hint + " Обязательное поле.")
	}
	if entry.Min != nil {
		hint += fmt.Sprintf(" Минимум: %d.", *entry.Min)
	}
	if entry.Max != nil {
		hint += fmt.Sprintf(" Максимум: %d.", *entry.Max)
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
		raw:     &stored,
		hint:    hint,
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
	if m.settings.busy {
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
		m.settings.passwordConfirmation = ""
	}
	if m.settings.review {
		return m.updateSettingsReview(msg)
	}
	if m.settings.compound != nil {
		return m.updateSettingsCompound(msg)
	}
	if m.settings.pendingDiscard {
		if key, ok := msg.(tea.KeyMsg); ok {
			m.settings.pendingDiscard = false
			m.settings.menu.notice = ""
			if key.String() == "y" {
				m.settings.patch = nil
				if m.settings.leaveAfterDiscard {
					m.state = stateMain
					if m.settings.build != "" {
						m.state = stateCard
					}
					return m, nil
				}
				return m.loadSettings(nil)
			}
		}
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && (m.settings.pendingRestart || m.settings.pendingRemove != "" || m.settings.pendingClear != "") {
		if key.String() == "y" {
			return m.settingsKey("y", "", "")
		}
		m.settings.pendingRestart, m.settings.pendingRemove, m.settings.pendingClear = false, "", ""
		m.settings.menu.notice = ""
	}
	before := m.settings.menu
	updated, event := m.settings.menu.Update(msg)
	m.settings.menu = updated
	if event.kind == menuSubmitted {
		m.settings.retryMenu = &before
	}
	if event.kind == menuNothing && m.settings.menu.mode != menuTyping {
		m.settings.passwordConfirmation = ""
	}
	kind, path := splitID(event.id)

	switch event.kind {
	case menuOpened:
		return m.openSettings(kind, path, event.id)
	case menuSubmitted:
		m.settings.menu.problem, m.settings.menu.notice = "", ""
		switch kind {
		case "search":
			if strings.TrimSpace(event.value) == "" {
				return m, nil
			}
			m.settings.stack = append(m.settings.stack, settingsLevel{title: "Поиск: " + event.value, query: event.value})
			return m.loadSettings(m.settings.patch)
		case "field":
			if path == "console.password" {
				if m.settings.passwordConfirmation == "" {
					m.settings.passwordConfirmation = event.value
					m.settings.menu.startEditing()
					m.settings.menu.input.Placeholder = "повторите пароль"
					m.settings.menu.notice = "повторите новый пароль"
					return m, nil
				}
				password := m.settings.passwordConfirmation
				m.settings.passwordConfirmation = ""
				if password != event.value {
					m.settings.menu.problem = "пароли не совпали; введите заново"
					return m, nil
				}
				return m.stageSetting(path, event.value, adminv1.SettingWriteMode_SETTING_WRITE_MODE_PASSWORD)
			}
			mode := adminv1.SettingWriteMode_SETTING_WRITE_MODE_EDIT_REDACTED
			if item, ok := m.settings.menu.current(); ok && item.editor == editSecret {
				mode = adminv1.SettingWriteMode_SETTING_WRITE_MODE_REPLACE
			}
			return m.stageSetting(path, event.value, mode)
		case "add":
			if event.value == "" {
				return m, nil
			}
			return m.stageChange(&adminv1.SettingChange{Action: &adminv1.SettingChange_Add{Add: &adminv1.AddSettingEntryRequest{Collection: path, Name: event.value}}})
		}
		return m, nil
	case menuPressed:
		return m.settingsKey(event.key, kind, path)
	}
	return m, nil
}

func (m Model) openSettings(kind, path, id string) (tea.Model, tea.Cmd) {
	m.settings.menu.problem, m.settings.menu.notice = "", ""
	m.settings.pendingRestart, m.settings.pendingRemove, m.settings.pendingClear = false, "", ""
	here := m.settings.here()
	label := ""
	if item, ok := m.settings.menu.current(); ok {
		label = item.label
	}
	push := func(level settingsLevel) (tea.Model, tea.Cmd) {
		m.settings.stack[len(m.settings.stack)-1].cursor = m.settings.menu.cursor
		m.settings.stack = append(m.settings.stack, level)
		m.settings.menu.cursor, m.settings.menu.first = 0, 0
		return m.loadSettings(m.settings.patch)
	}
	switch kind {
	case "field":
		return m.openSettingsCompound(path)
	case "section":
		return push(settingsLevel{title: label, section: path})
	case "coll":
		return push(settingsLevel{title: label, section: here.section, entry: here.entry, collection: path})
	case "item":
		return push(settingsLevel{title: label, entry: path})
	case "add":
		return m.stageChange(&adminv1.SettingChange{Action: &adminv1.SettingChange_Add{Add: &adminv1.AddSettingEntryRequest{Collection: path}}})
	}
	return m, nil
}

func (m Model) settingsKey(key, kind, path string) (tea.Model, tea.Cmd) {
	switch key {
	case "s":
		return m.reviewSettings()
	case "d":
		if m.settingsDirty() {
			m.settings.pendingDiscard, m.settings.leaveAfterDiscard = true, false
			m.settings.menu.notice = "отменить все несохранённые изменения? y — отменить"
		}
		return m, nil
	case "esc", "q":
		if len(m.settings.stack) > 1 {
			m.settings.stack = m.settings.stack[:len(m.settings.stack)-1]
			m.settings.menu.cursor = m.settings.here().cursor
			m.settings.menu.first = 0
			m.settings.menu.problem, m.settings.menu.notice = "", ""
			return m.loadSettings(m.settings.patch)
		}
		if m.settingsDirty() {
			m.settings.pendingDiscard, m.settings.leaveAfterDiscard = true, true
			m.settings.menu.notice = "выйти без сохранения? y — выйти; другая клавиша — остаться"
			return m, nil
		}
		m.state = stateMain
		if m.settings.build != "" {
			m.state = stateCard
		}
		return m, nil
	case "r":
		if m.settings.build != "" {
			m.settings.menu.notice = "настройки сборки вступают в силу после публикации"
			return m, nil
		}
		if m.settingsDirty() {
			m.settings.menu.problem = "сначала сохраните изменения (s) или отмените их (d)"
			return m, nil
		}
		m.settings.pendingRestart = true
		m.settings.menu.notice = "перезапустить проект? y — перезапустить, любая другая — нет"
		return m, nil
	case "y":
		if m.settings.pendingClear != "" {
			target := m.settings.pendingClear
			m.settings.pendingClear = ""
			m.settings.menu.notice = ""
			return m.stageSetting(target, "", adminv1.SettingWriteMode_SETTING_WRITE_MODE_CLEAR)
		}
		if m.settings.pendingRestart {
			m.settings.pendingRestart = false
			m.settings.menu.notice = "перезапускаю проект…"
			return m, restartProject(m.ctx, m.client)
		}
		if m.settings.pendingRemove != "" {
			target := m.settings.pendingRemove
			m.settings.pendingRemove = ""
			return m.stageChange(&adminv1.SettingChange{Action: &adminv1.SettingChange_Remove{Remove: &adminv1.RemoveSettingEntryRequest{Path: target}}})
		}
		return m, nil
	case "x":
		switch kind {
		case "field":
			if item, ok := m.settings.menu.current(); ok && item.editor == editSecret {
				m.settings.pendingClear = path
				m.settings.menu.notice = "очистить «" + item.label + "»? y — очистить, любая другая — нет"
				return m, nil
			}
			return m.stageSetting(path, "", adminv1.SettingWriteMode_SETTING_WRITE_MODE_CLEAR)
		case "item":
			m.settings.pendingRemove = path
			label := path
			if item, ok := m.settings.menu.current(); ok {
				label = item.label
			}
			m.settings.menu.notice = "удалить «" + label + "»? y — удалить, любая другая — нет"
			if m.settings.here().collection == "modules.config" {
				m.settings.menu.notice = "сбросить настройки «" + label + "»? Файл модуля останется. y — сбросить, другая клавиша — отменить"
			}
			return m, nil
		}
		return m, nil
	}
	m.settings.pendingRestart = false
	m.settings.pendingRemove = ""
	m.settings.pendingClear = ""
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
	return m.loadSettings(m.settings.patch)
}

func (m Model) settingsView() string {
	return m.centered(m.settings.menu.View(m.styles, m.icons))
}

func (m Model) settingsBar() string {
	item := func(letter, label string) string {
		return m.styles.key.Render(letter) + " " + m.styles.keyLabel.Render(label)
	}
	if m.settings.compound != nil && m.settings.menu.mode == menuBrowsing {
		return m.styles.bar.Render(fitBar([]string{item("Enter", "изменить"), item("x", "удалить")}, []string{item("Esc", "к настройкам")}, m.width-4))
	}
	if m.settings.review {
		return m.styles.bar.Render(fitBar([]string{item("s", "сохранить всё"), item("e", "экспорт")}, []string{item("Esc", "к правкам")}, m.width-4))
	}
	if m.settingsDirty() && m.settings.menu.mode == menuBrowsing {
		return m.styles.bar.Render(fitBar([]string{item("Enter", "изменить"), item("s", "проверить"), item("d", "отменить всё")}, []string{item("Esc", "назад")}, m.width-4))
	}
	switch m.settings.menu.mode {
	case menuMultiline:
		return m.styles.bar.Render(fitBar([]string{item("Ctrl+S", "принять JSON")}, []string{item("Esc", "отменить")}, m.width-4))
	case menuTyping:
		if current, ok := m.settings.menu.current(); ok && current.editor == editSecret {
			return m.styles.bar.Render(fitBar([]string{item("Enter", "заменить"), item("пусто", "не менять")}, []string{item("Esc", "отменить")}, m.width-4))
		}
		return m.styles.bar.Render(strings.Join([]string{item("Enter", "принять правку"), item("Esc", "отменить")}, "   "))
	case menuChoosing:
		return m.styles.bar.Render(strings.Join([]string{item("↑↓", "выбрать"), item("Enter", "принять"), item("Esc", "отменить")}, "   "))
	}
	left := []string{item("Enter", "открыть"), item("↑↓", "выбрать")}
	if current, ok := m.settings.menu.current(); ok {
		kind, _ := splitID(current.id)
		switch kind {
		case "field":
			reset := "сбросить"
			if current.editor == editSecret {
				reset = "очистить"
			}
			left = []string{item("Enter", "изменить"), item("↑↓", "выбрать"), item("x", reset)}
		case "item":
			remove := "удалить"
			if m.settings.here().collection == "modules.config" {
				remove = "сбросить настройки"
			}
			left = []string{item("Enter", "открыть"), item("↑↓", "выбрать"), item("x", remove)}
		}
	}
	right := []string{item("r", "перезапустить"), item("Esc", "назад")}
	return m.styles.bar.Render(fitBar(left, right, m.width-4))
}
