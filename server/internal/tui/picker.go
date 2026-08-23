package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type pickItem struct {
	label string
	value string
	hint  string
}

type picker struct {
	title    string
	items    []pickItem
	filtered []pickItem
	filter   textinput.Model
	cursor   int
	rows     int
	icons    iconSet
	styles   styles
}

func newPicker(title string, items []pickItem, ic iconSet, st styles) picker {
	input := textinput.New()
	input.Placeholder = "фильтр…"
	input.Prompt = ""
	input.Focus()
	p := picker{title: title, items: items, filter: input, rows: 12, icons: ic, styles: st}
	p.apply()
	return p
}

func (p *picker) apply() {
	query := strings.ToLower(strings.TrimSpace(p.filter.Value()))
	p.filtered = p.filtered[:0]
	for _, item := range p.items {
		if query == "" || strings.Contains(strings.ToLower(item.label), query) || strings.Contains(strings.ToLower(item.hint), query) {
			p.filtered = append(p.filtered, item)
		}
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = max(0, len(p.filtered)-1)
	}
}

func (p picker) Update(msg tea.Msg) (picker, *pickItem) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "ctrl+p":
			if p.cursor > 0 {
				p.cursor--
			}
			return p, nil
		case "down", "ctrl+n":
			if p.cursor < len(p.filtered)-1 {
				p.cursor++
			}
			return p, nil
		case "enter":
			if p.cursor < len(p.filtered) {
				selected := p.filtered[p.cursor]
				return p, &selected
			}
			return p, nil
		}
	}
	var cmd tea.Cmd
	p.filter, cmd = p.filter.Update(msg)
	_ = cmd
	p.apply()
	return p, nil
}

func (p picker) View() string {
	var b strings.Builder
	b.WriteString(p.styles.wizardTitle.Render(p.title) + "\n")
	b.WriteString(p.icons.search + " " + p.filter.View() + "\n\n")

	start := 0
	if p.cursor >= p.rows {
		start = p.cursor - p.rows + 1
	}
	end := min(start+p.rows, len(p.filtered))
	for i := start; i < end; i++ {
		item := p.filtered[i]
		row := item.label
		if item.hint != "" {
			row += "  " + p.styles.dim.Render(item.hint)
		}
		if i == p.cursor {
			b.WriteString(p.styles.selected.Render(p.icons.cursor+" "+row) + "\n")
		} else {
			b.WriteString("  " + row + "\n")
		}
	}
	if len(p.filtered) == 0 {
		b.WriteString(p.styles.dim.Render("  ничего не найдено") + "\n")
	}
	return b.String()
}
