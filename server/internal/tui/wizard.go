package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/platform"
)

type wizardStep int

const (
	wzVersion wizardStep = iota
	wzLoader
	wzLoaderVersion
	wzRecipe
	wzName
	wzPlatforms
	wzReview
)

const recipeDecline = "-"

type wizardRequest struct {
	step wizardStep
}

type wizard struct {
	ctx    context.Context
	client adminv1connect.AdminServiceClient
	icons  iconSet
	styles styles

	step    wizardStep
	loading bool
	pick    picker
	name    textinput.Model
	request *wizardRequest
	problem string

	mc            string
	loader        string
	loaderVersion string
	recipe        string
	loaderIndex   map[string][]string
	recipes       []*adminv1.RecipeInfo
	history       []wizard
	platforms     []string
	platformMenu  menu
	current       *adminv1.BuildInfo

	done        bool
	cancel      bool
	commandLine string
}

func newWizard(ctx context.Context, client adminv1connect.AdminServiceClient, ic iconSet, st styles, prefill string) (wizard, tea.Cmd) {
	name := textinput.New()
	name.Placeholder = "имя сборки"
	name.Prompt = ""
	name.SetValue(prefill)
	w := wizard{ctx: ctx, client: client, icons: ic, styles: st, loading: true, name: name, request: &wizardRequest{step: wzVersion}, platforms: []string{"windows-x64"}}
	return w, fetchVersions(ctx, client, w.request, prefill)
}

func (w wizard) Update(msg tea.Msg) (wizard, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "esc" {
			return w.back(), nil
		}
		if msg.String() == "enter" && w.problem != "" && w.request != nil {
			if w.request.step == wzVersion {
				return newWizard(w.ctx, w.client, w.icons, w.styles, w.name.Value())
			}
			w.problem, w.loading = "", true
			w.request = &wizardRequest{step: wzLoader}
			return w, fetchLoaders(w.ctx, w.client, w.mc, w.request)
		}
	case versionsMsg:
		if !w.loading || msg.request != w.request {
			return w, nil
		}
		if msg.err != nil {
			w.loading, w.problem = false, errorText(msg.err)
			return w, nil
		}
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
		w.current = msg.current
		if w.current != nil {
			w.pick.selectValue(w.current.MinecraftVersion)
			if words, err := command.Split(w.current.Install); err == nil && len(words) > 0 {
				w.pick.selectValue(words[0])
			}
			if keys := platform.Keys(w.current.Prepared); len(keys) > 0 {
				w.platforms = keys
			}
		}
		w.step = wzVersion
		w.loading = false
		return w, nil
	case loadersMsg:
		if !w.loading || msg.request != w.request || msg.minecraftVersion != w.mc {
			return w, nil
		}
		if msg.err != nil {
			w.loading, w.problem = false, errorText(msg.err)
			return w, nil
		}
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
		if w.current != nil {
			w.pick.selectValue(strings.ToLower(w.current.Loader))
			if value := w.installOption("loader"); value != "" {
				w.pick.selectValue(value)
			}
		}
		w.step = wzLoader
		w.loading = false
		return w, nil
	case errMsg:
		w.loading = false
		return w, nil
	}

	if w.loading || w.problem != "" {
		return w, nil
	}
	if w.step == wzPlatforms {
		return w.updatePlatforms(msg)
	}
	if w.step == wzReview {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "enter" {
			w.commandLine, w.done = w.buildCommand(), true
		}
		return w, nil
	}

	if w.step == wzName {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "enter" {
			if strings.TrimSpace(w.name.Value()) == "" {
				return w, nil
			}
			w.history = append(slices.Clone(w.history), w)
			w.step = wzPlatforms
			w.platformMenu = newMenu("На каких системах будут играть?")
			w.platformMenu.resize(80, 19)
			w.fillPlatforms()
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
	w.problem = ""
	w.history = append(slices.Clone(w.history), w)
	switch w.step {
	case wzVersion:
		w.mc = value
		w.loader, w.loaderVersion, w.recipe = "", "", ""
		w.loading = true
		w.request = &wizardRequest{step: wzLoader}
		return w, fetchLoaders(w.ctx, w.client, w.mc, w.request)
	case wzLoader:
		w.loader = value
		w.loaderVersion, w.recipe = "", ""
		return w.askRecipe()
	case wzRecipe:
		if value == recipeDecline {
			w.recipe = "нет"
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

func (w wizard) back() wizard {
	if len(w.history) == 0 {
		w.cancel = true
		return w
	}
	previous := w.history[len(w.history)-1]
	previous.name = w.name
	previous.name.Blur()
	previous.loading = false
	previous.request = nil
	previous.problem = ""
	previous.platforms = slices.Clone(w.platforms)
	if previous.step == wzPlatforms {
		previous.fillPlatforms()
	}
	return previous
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
	if w.loader == w.installOption("loader") {
		w.pick.selectValue(w.installOption("loaderVersion"))
	}
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
	if w.current != nil {
		value := w.current.Compat
		if value == "" {
			value = recipeDecline
		}
		w.pick.selectValue(value)
	}
	w.step = wzRecipe
	return w, nil
}

func (w wizard) buildCommand() string {
	return command.Format(w.buildArguments()...)
}

func (w wizard) buildArguments() []string {
	arguments := []string{"install", strings.TrimSpace(w.name.Value()), w.mc}
	if w.loader != "" && w.loader != "vanilla" {
		arguments = append(arguments, "loader="+w.loader)
	}
	if w.loaderVersion != "" {
		arguments = append(arguments, "loaderVersion="+w.loaderVersion)
	}
	if w.recipe != "" {
		arguments = append(arguments, "compat="+w.recipe)
	}
	if len(w.platforms) > 0 {
		arguments = append(arguments, "platform="+strings.Join(w.platforms, ","))
	}
	return arguments
}

func (w wizard) View(bounds ...int) string {
	width, height := 80, 23
	if len(bounds) == 2 {
		width, height = bounds[0], bounds[1]
	}
	inner := max(20, min(width-8, 74))
	w.name.Width = inner - 2
	if w.step == wzPlatforms {
		w.platformMenu.resize(width-6, height-4)
		return w.styles.wizardBox.Render(w.platformMenu.View(w.styles, w.icons))
	}
	if w.step == wzReview {
		return w.styles.wizardBox.Render(w.styles.wizardTitle.Render("Проверьте сборку") + "\n\n" + wrapText(w.buildCommand(), inner, max(2, height-10)) + "\n\n" + w.styles.dim.Render(wrapText("Enter — начать сборку, Esc — изменить платформы", inner, 2)))
	}
	if w.problem != "" {
		return w.styles.wizardBox.Render(w.styles.bad.Render(w.problem) + "\n\n" + w.styles.dim.Render("Enter — повторить, Esc — назад"))
	}
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
		if w.recipe != "" && w.recipe != "нет" {
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
