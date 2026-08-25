package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type menuTone int

const (
	toneNormal menuTone = iota
	toneMuted
	toneGood
	toneWarn
	toneBad
)

type menuEditor int

const (
	editNone menuEditor = iota
	editText
	editSecret
	editSelect
	editToggle
)

type menuItem struct {
	id      string
	label   string
	value   string
	note    string
	hint    string
	tone    menuTone
	editor  menuEditor
	options []string
	static  bool
	action  bool
}

type menuMode int

const (
	menuBrowsing menuMode = iota
	menuTyping
	menuChoosing
)

type menuEventKind int

const (
	menuNothing menuEventKind = iota
	menuOpened
	menuSubmitted
	menuPressed
)

type menuEvent struct {
	kind  menuEventKind
	id    string
	value string
	key   string
}

type menu struct {
	title    string
	subtitle string
	empty    string
	items    []menuItem
	cursor   int
	first    int
	width    int
	height   int
	mode     menuMode
	input    textinput.Model
	choice   int
	notice   string
	problem  string
}

func (m *menu) resize(width, height int) {
	m.width = min(max(width-12, 40), 96)
	m.height = max(height, menuChrome+3)
	m.scroll()
}

func (m menu) rows() int {
	rows := max(m.height-menuChrome, 3)
	if m.mode == menuChoosing {
		if item, ok := m.current(); ok {
			rows = max(rows-len(item.options), 2)
		}
	}
	return rows
}

func (m *menu) scroll() {
	rows := m.rows()
	if len(m.items) <= rows {
		m.first = 0
		return
	}
	if m.cursor < m.first {
		m.first = m.cursor
	}
	if m.cursor >= m.first+rows {
		m.first = m.cursor - rows + 1
	}
	if m.first > len(m.items)-rows {
		m.first = len(m.items) - rows
	}
	if m.first < 0 {
		m.first = 0
	}
}

func newMenu(title string) menu {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 1024
	return menu{title: title, input: input}
}

func (m *menu) setItems(items []menuItem) {
	m.items = items
	m.clamp()
	m.scroll()
}

func (m *menu) clamp() {
	if len(m.items) == 0 {
		m.cursor, m.first = 0, 0
		return
	}
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if !m.items[m.cursor].static {
		return
	}
	for index := m.cursor; index < len(m.items); index++ {
		if !m.items[index].static {
			m.cursor = index
			return
		}
	}
	for index := m.cursor; index >= 0; index-- {
		if !m.items[index].static {
			m.cursor = index
			return
		}
	}
}

func (m menu) current() (menuItem, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return menuItem{}, false
	}
	return m.items[m.cursor], true
}

func (m *menu) move(delta int) {
	if len(m.items) == 0 {
		return
	}
	if m.cursor < 0 {
		m.cursor = -1
	}
	if m.cursor > len(m.items) {
		m.cursor = len(m.items)
	}
	next := m.cursor
	for step := 0; step < len(m.items); step++ {
		next = (next + delta + len(m.items)) % len(m.items)
		if !m.items[next].static {
			m.cursor = next
			m.scroll()
			return
		}
	}
}

func (m *menu) startEditing() {
	item, ok := m.current()
	if !ok {
		return
	}
	switch item.editor {
	case editSelect:
		m.mode = menuChoosing
		m.choice = 0
		for index, option := range item.options {
			if option == item.value {
				m.choice = index
			}
		}
	case editText, editSecret:
		m.mode = menuTyping
		if item.editor == editSecret {
			m.input.SetValue("")
		} else {
			m.input.SetValue(item.value)
		}
		m.input.Width = max(m.width/2, 24)
		m.input.CursorEnd()
		m.input.Focus()
	}
	m.scroll()
}

func (m menu) Update(msg tea.Msg) (menu, menuEvent) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, menuEvent{}
	}
	item, hasItem := m.current()

	switch m.mode {
	case menuTyping:
		switch key.String() {
		case "esc":
			m.mode = menuBrowsing
			m.input.Blur()
			return m, menuEvent{}
		case "enter":
			m.mode = menuBrowsing
			m.input.Blur()
			return m, menuEvent{kind: menuSubmitted, id: item.id, value: strings.TrimSpace(m.input.Value())}
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		_ = cmd
		return m, menuEvent{}
	case menuChoosing:
		switch key.String() {
		case "esc":
			m.mode = menuBrowsing
			return m, menuEvent{}
		case "up", "k":
			if m.choice > 0 {
				m.choice--
			}
			return m, menuEvent{}
		case "down", "j":
			if m.choice < len(item.options)-1 {
				m.choice++
			}
			return m, menuEvent{}
		case "enter":
			m.mode = menuBrowsing
			if m.choice < len(item.options) {
				return m, menuEvent{kind: menuSubmitted, id: item.id, value: item.options[m.choice]}
			}
			return m, menuEvent{}
		}
		return m, menuEvent{}
	}

	switch key.String() {
	case "up", "k":
		m.move(-1)
		return m, menuEvent{}
	case "down", "j":
		m.move(1)
		return m, menuEvent{}
	case "home", "g":
		m.cursor = -1
		m.move(1)
		return m, menuEvent{}
	case "end", "G":
		m.cursor = len(m.items)
		m.move(-1)
		return m, menuEvent{}
	case "pgup":
		for step := 0; step < m.rows(); step++ {
			m.move(-1)
		}
		return m, menuEvent{}
	case "pgdown":
		for step := 0; step < m.rows(); step++ {
			m.move(1)
		}
		return m, menuEvent{}
	case "enter":
		if !hasItem {
			return m, menuEvent{}
		}
		switch item.editor {
		case editToggle:
			return m, menuEvent{kind: menuSubmitted, id: item.id, value: flipWord(item.value)}
		case editText, editSecret, editSelect:
			m.startEditing()
			return m, menuEvent{}
		default:
			return m, menuEvent{kind: menuOpened, id: item.id}
		}
	}
	return m, menuEvent{kind: menuPressed, id: item.id, key: key.String()}
}

func flipWord(value string) string {
	if value == "да" {
		return "нет"
	}
	return "да"
}

const (
	menuChrome   = 8
	menuHintRows = 2
)

func (m menu) View(st styles, icons iconSet) string {
	rows := m.rows()
	first := m.first
	last := min(first+rows, len(m.items))

	labelWidth := 0
	for _, item := range m.items {
		labelWidth = max(labelWidth, lipgloss.Width(item.label))
	}
	labelWidth = min(labelWidth, m.width/2)

	lines := []string{st.selected.Render(m.title)}
	if m.subtitle != "" {
		lines = append(lines, st.faint.Render(m.subtitle))
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, "")

	if first > 0 {
		lines = append(lines, st.faint.Render("  ↑ выше ещё "+itoa(first)))
	} else {
		lines = append(lines, "")
	}
	if len(m.items) == 0 {
		lines = append(lines, st.dim.Render("  "+m.empty))
	}
	for index := first; index < last; index++ {
		item := m.items[index]
		lines = append(lines, m.renderRow(item, index, labelWidth, st, icons))
		if m.mode == menuChoosing && index == m.cursor {
			lines = append(lines, m.renderOptions(item, st, icons)...)
		}
	}
	if last < len(m.items) {
		lines = append(lines, st.faint.Render("  ↓ ниже ещё "+itoa(len(m.items)-last)))
	} else {
		lines = append(lines, "")
	}

	hint := ""
	if item, ok := m.current(); ok {
		hint = item.hint
	}
	footer := st.faint.Render(wrapText(hint, m.width, menuHintRows))
	if m.problem != "" {
		footer = st.bad.Render(wrapText(m.problem, m.width, menuHintRows))
	} else if m.notice != "" {
		footer = st.warn.Render(wrapText(m.notice, m.width, menuHintRows))
	}

	body := lipgloss.NewStyle().Width(m.width).Height(max(m.height-menuHintRows-1, 1)).Render(strings.Join(lines, "\n"))
	return lipgloss.NewStyle().Width(m.width).Render(body + "\n" + footer)
}

func (m menu) renderRow(item menuItem, index, labelWidth int, st styles, icons iconSet) string {
	mark := "  "
	label := item.label
	if index == m.cursor && !item.static {
		mark = st.selected.Render(icons.cursor + " ")
	}
	padding := max(labelWidth-lipgloss.Width(label), 0)
	switch {
	case item.action:
		label = st.good.Render("+ " + label)
	case item.static:
		label = st.dim.Render(label + strings.Repeat(" ", padding))
	case index == m.cursor:
		label = st.selected.Render(label + strings.Repeat(" ", padding))
	default:
		label = st.keyLabel.Render(label + strings.Repeat(" ", padding))
	}

	value := item.value
	if m.mode == menuTyping && index == m.cursor {
		return clip(mark+label+"   "+m.input.View(), m.width)
	}
	if value == "" {
		return clip(mark+label, m.width)
	}
	row := mark + label + "   " + toned(value, item.tone, st)
	if item.note != "" {
		row += "   " + st.faint.Render(item.note)
	}
	return clip(row, m.width)
}

func clip(text string, width int) string {
	if width <= 0 {
		return text
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(text)
}

func toned(value string, tone menuTone, st styles) string {
	switch tone {
	case toneMuted:
		return st.dim.Render(value)
	case toneGood:
		return st.good.Render(value)
	case toneWarn:
		return st.warn.Render(value)
	case toneBad:
		return st.bad.Render(value)
	default:
		return st.keyLabel.Render(value)
	}
}

func (m menu) renderOptions(item menuItem, st styles, icons iconSet) []string {
	lines := make([]string, 0, len(item.options))
	for index, option := range item.options {
		row := "      " + option
		if index == m.choice {
			row = "    " + st.selected.Render(icons.cursor+" "+option)
		} else {
			row = "      " + st.dim.Render(option)
		}
		if option == item.value {
			row += st.faint.Render("   сейчас")
		}
		lines = append(lines, clip(row, m.width))
	}
	return lines
}

func wrapText(text string, width, rows int) string {
	if text == "" {
		return ""
	}
	wrapped := lipgloss.NewStyle().Width(width).Render(text)
	lines := strings.Split(wrapped, "\n")
	if len(lines) > rows {
		lines = lines[:rows]
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
