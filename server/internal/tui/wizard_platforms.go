package tui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/platform"
)

func (w wizard) installOption(key string) string {
	if w.current == nil {
		return ""
	}
	words, err := command.Split(w.current.Install)
	if err != nil {
		return ""
	}
	for _, word := range words {
		if value, ok := strings.CutPrefix(word, key+"="); ok {
			return value
		}
	}
	return ""
}

func (w *wizard) fillPlatforms() {
	var items []menuItem
	for _, value := range platform.Game() {
		key, _ := platform.Key(value)
		word := "нет"
		if slices.Contains(w.platforms, key) {
			word = "да"
		}
		items = append(items, menuItem{id: key, label: key, value: word, editor: editToggle})
	}
	items = append(items, menuItem{id: "next", label: "проверить и продолжить", action: true})
	w.platformMenu.setItems(items)
}

func (w wizard) updatePlatforms(message tea.Msg) (wizard, tea.Cmd) {
	menu, event := w.platformMenu.Update(message)
	w.platformMenu = menu
	if event.kind == menuSubmitted {
		w.platforms = slices.Clone(w.platforms)
		if event.value == "да" {
			w.platforms = append(w.platforms, event.id)
		} else {
			w.platforms = slices.DeleteFunc(w.platforms, func(value string) bool { return value == event.id })
		}
		w.platformMenu.problem = ""
		w.fillPlatforms()
	}
	if event.kind == menuOpened && event.id == "next" {
		if len(w.platforms) == 0 {
			w.platformMenu.problem = "выберите хотя бы одну платформу"
			return w, nil
		}
		w.history = append(slices.Clone(w.history), w)
		w.step = wzReview
	}
	return w, nil
}

func (p *picker) selectValue(value string) {
	for index, item := range p.filtered {
		if item.value == value {
			p.cursor = index
			return
		}
	}
}
