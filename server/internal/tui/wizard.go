package tui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
)

type wizardStep int

const (
	wzVersion wizardStep = iota
	wzLoader
	wzLoaderVersion
	wzRecipe
	wzName
)

const recipeDecline = "-"

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
	recipe        string
	loaderIndex   map[string][]string
	recipes       []*adminv1.RecipeInfo

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
			hint := versionHint(v.Type)
			if v.Id == msg.latestRelease {
				hint = "последний релиз"
			}
			items = append(items, pickItem{label: v.Id, value: v.Id, hint: hint})
		}
		sortLatestFirst(items, msg.latestRelease)
		w.pick = newPicker("Какую версию Minecraft собрать?", items, w.icons, w.styles)
		w.step = wzVersion
		w.loading = false
		return w, nil
	case loadersMsg:
		w.loaderIndex = map[string][]string{}
		w.recipes = msg.recipes
		items := make([]pickItem, 0, len(msg.loaders))
		for _, l := range msg.loaders {
			if l.Trouble != "" {
				continue
			}
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
		w.pick = newPicker("С каким загрузчиком модов?", items, w.icons, w.styles)
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
		return w.askRecipe()
	case wzRecipe:
		if value == recipeDecline {
			return w.askLoaderVersion()
		}
		w.recipe = value
		return w.askName()
	case wzLoaderVersion:
		w.loaderVersion = value
		return w.askName()
	}
	return w, nil
}

func (w wizard) fittingRecipes() []*adminv1.RecipeInfo {
	var fitting []*adminv1.RecipeInfo
	for _, recipe := range w.recipes {
		if recipe.Loader == w.loader {
			fitting = append(fitting, recipe)
		}
	}
	return fitting
}

func (w wizard) askName() (wizard, tea.Cmd) {
	w.step = wzName
	w.name.Focus()
	return w, textinput.Blink
}

func (w wizard) askLoaderVersion() (wizard, tea.Cmd) {
	versions := w.loaderIndex[w.loader]
	if w.loader == "vanilla" || len(versions) == 0 {
		return w.askName()
	}
	items := make([]pickItem, 0, len(versions))
	for _, version := range versions {
		items = append(items, pickItem{label: version, value: version})
	}
	w.pick = newPicker("Версия "+w.loader+" — какую взять?", items, w.icons, w.styles)
	w.step = wzLoaderVersion
	return w, nil
}

func (w wizard) askRecipe() (wizard, tea.Cmd) {
	fitting := w.fittingRecipes()
	if len(fitting) == 0 {
		return w.askLoaderVersion()
	}
	items := make([]pickItem, 0, len(fitting)+1)
	for _, recipe := range fitting {
		items = append(items, pickItem{label: recipe.Name, value: recipe.Name, hint: recipe.Summary})
	}
	items = append(items, pickItem{label: "без рецепта", value: recipeDecline, hint: "как эта версия выходила — Java 8 и старый LWJGL"})
	w.pick = newPicker("Эта версия старая. Запустить её на современной Java?", items, w.icons, w.styles)
	w.step = wzRecipe
	return w, nil
}

func (w wizard) buildCommand() string {
	command := fmt.Sprintf("install %s %s", w.name.Value(), w.mc)
	if w.loader != "" && w.loader != "vanilla" {
		command += " loader=" + w.loader
	}
	if w.loaderVersion != "" {
		command += " loaderVersion=" + w.loaderVersion
	}
	if w.recipe != "" {
		command += " compat=" + w.recipe
	}
	return command
}

func (w wizard) View() string {
	if w.loading {
		return w.styles.wizardBox.Render(w.styles.dim.Render("Спрашиваю проект…"))
	}
	if w.step == wzName {
		summary := w.mc
		if w.loader != "" && w.loader != "vanilla" {
			summary += "  ·  " + w.loader + " " + w.loaderVersion
		} else {
			summary += "  ·  vanilla"
		}
		if w.recipe != "" {
			summary += "  ·  " + w.recipe
		}
		body := w.styles.wizardTitle.Render("Как назвать сборку?") + "\n" +
			w.styles.dim.Render(summary) + "\n\n" +
			w.styles.selected.Render("› ") + w.name.View() + "\n\n" +
			w.styles.faint.Render("Это имя увидит игрок в лаунчере.")
		return w.styles.wizardBox.Render(body)
	}
	return w.styles.wizardBox.Render(w.pick.View())
}

func versionHint(kind string) string {
	switch kind {
	case "release":
		return "релиз"
	case "snapshot":
		return "снапшот"
	default:
		return kind
	}
}

func sortLatestFirst(items []pickItem, latest string) {
	if latest == "" {
		return
	}
	for i, item := range items {
		if item.value != latest {
			continue
		}
		copy(items[1:i+1], items[:i])
		items[0] = item
		return
	}
}
