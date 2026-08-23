package tui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
)

type wizardStep int

const (
	wzVersion wizardStep = iota
	wzLoader
	wzLoaderVersion
	wzName
)

type wizard struct {
	ctx    context.Context
	client adminv1connect.AdminServiceClient
	icons  iconSet
	styles styles

	step    wizardStep
	loading bool
	pick    picker
	name    textinput.Model

	mc            string
	loader        string
	loaderVersion string
	loaderIndex   map[string][]string

	done        bool
	cancel      bool
	commandLine string
}

func newWizard(ctx context.Context, client adminv1connect.AdminServiceClient, ic iconSet, st styles, prefill string) (wizard, tea.Cmd) {
	name := textinput.New()
	name.Placeholder = "имя сборки"
	name.Prompt = ""
	name.SetValue(prefill)
	w := wizard{ctx: ctx, client: client, icons: ic, styles: st, loading: true, name: name}
	return w, fetchVersions(ctx, client)
}

func (w wizard) Update(msg tea.Msg) (wizard, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "esc" {
			w.cancel = true
			return w, nil
		}
	case versionsMsg:
		items := make([]pickItem, 0, len(msg.versions))
		for _, v := range msg.versions {
			items = append(items, pickItem{label: v.Id, value: v.Id, hint: v.Type})
		}
		w.pick = newPicker(w.icons.install+" Версия Minecraft", items, w.icons, w.styles)
		w.step = wzVersion
		w.loading = false
		return w, nil
	case loadersMsg:
		w.loaderIndex = map[string][]string{}
		items := make([]pickItem, 0, len(msg.loaders))
		for _, l := range msg.loaders {
			w.loaderIndex[l.Name] = l.Versions
			hint := "нет версий"
			if len(l.Versions) > 0 {
				hint = fmt.Sprintf("последняя %s", l.Versions[0])
			}
			if l.Name == "vanilla" {
				hint = "без модов"
			}
			items = append(items, pickItem{label: l.Name, value: l.Name, hint: hint})
		}
		w.pick = newPicker(w.icons.builds+" Загрузчик", items, w.icons, w.styles)
		w.step = wzLoader
		w.loading = false
		return w, nil
	case errMsg:
		w.loading = false
		return w, nil
	}

	if w.loading {
		return w, nil
	}

	if w.step == wzName {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "enter" {
			if w.name.Value() == "" {
				return w, nil
			}
			w.commandLine = w.buildCommand()
			w.done = true
			return w, nil
		}
		var cmd tea.Cmd
		w.name, cmd = w.name.Update(msg)
		return w, cmd
	}

	updated, selected := w.pick.Update(msg)
	w.pick = updated
	if selected == nil {
		return w, nil
	}
	return w.advance(selected.value)
}

func (w wizard) advance(value string) (wizard, tea.Cmd) {
	switch w.step {
	case wzVersion:
		w.mc = value
		w.loading = true
		return w, fetchLoaders(w.ctx, w.client, w.mc)
	case wzLoader:
		w.loader = value
		if value == "vanilla" {
			w.step = wzName
			w.name.Focus()
			return w, textinput.Blink
		}
		items := make([]pickItem, 0)
		for _, version := range w.loaderIndex[value] {
			items = append(items, pickItem{label: version, value: version})
		}
		w.pick = newPicker(w.icons.update+" Версия "+value, items, w.icons, w.styles)
		w.step = wzLoaderVersion
		return w, nil
	case wzLoaderVersion:
		w.loaderVersion = value
		w.step = wzName
		w.name.Focus()
		return w, textinput.Blink
	}
	return w, nil
}

func (w wizard) buildCommand() string {
	if w.loader == "" || w.loader == "vanilla" {
		return fmt.Sprintf("install %s %s", w.name.Value(), w.mc)
	}
	return fmt.Sprintf("install %s %s loader=%s loaderVersion=%s", w.name.Value(), w.mc, w.loader, w.loaderVersion)
}

func (w wizard) View() string {
	if w.loading {
		return w.styles.wizardBox.Render(w.styles.dim.Render("загружаю…"))
	}
	if w.step == wzName {
		summary := w.mc
		if w.loader != "" && w.loader != "vanilla" {
			summary += "  ·  " + w.loader + " " + w.loaderVersion
		} else {
			summary += "  ·  vanilla"
		}
		body := w.styles.wizardTitle.Render(w.icons.install+" Имя сборки") + "\n" +
			w.styles.dim.Render(summary) + "\n\n" +
			"› " + w.name.View() + "\n\n" +
			w.styles.dim.Render("Enter — собрать · Esc — отмена")
		return w.styles.wizardBox.Render(body)
	}
	return w.styles.wizardBox.Render(w.pick.View() + "\n" + w.styles.dim.Render("↑↓ выбор · Enter · Esc — отмена"))
}
