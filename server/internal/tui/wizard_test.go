package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/laminara/laminara/server/internal/command"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
)

func TestWizardIgnoresAbandonedRequestsIncludingErrors(t *testing.T) {
	w := freshWizard(nil)
	w, _ = w.advance("1.21.1")
	oldRequest := w.request
	w = w.back()
	w, _ = w.advance("1.21.1")
	for _, message := range []tea.Msg{
		loadersMsg{request: oldRequest, minecraftVersion: "1.21.1", err: errors.New("old failure")},
		loadersMsg{request: oldRequest, minecraftVersion: "1.21.1", loaders: []*adminv1.LoaderInfo{{Name: "vanilla"}}},
		versionsMsg{request: oldRequest, err: errors.New("old version failure")},
		versionsMsg{request: oldRequest, versions: []*adminv1.VersionInfo{{Id: "1.7.10"}}},
	} {
		model := Model{state: stateWizard, wizard: w}
		updated, _ := model.Update(message)
		got := updated.(Model)
		if got.state != stateWizard || !got.wizard.loading || got.wizard.problem != "" || got.wizard.mc != "1.21.1" {
			t.Fatalf("abandoned result changed the current wizard: %T", message)
		}
	}
}

func TestWizardCanRetryCurrentFailures(t *testing.T) {
	for _, step := range []wizardStep{wzVersion, wzLoader} {
		w, _ := newWizard(context.Background(), nil, unicodeIcons(), newStyles(), "My Pack")
		var message tea.Msg = versionsMsg{request: w.request, err: errors.New("network unavailable")}
		if step == wzLoader {
			w, _ = w.advance("1.21.1")
			message = loadersMsg{request: w.request, minecraftVersion: w.mc, err: errors.New("network unavailable")}
		}
		w, _ = w.Update(message)
		if w.loading || w.problem == "" || !strings.Contains(w.View(), "Enter") {
			t.Fatal("current failure must offer retry")
		}
		oldRequest, historySize := w.request, len(w.history)
		w, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if !w.loading || w.problem != "" || w.request == oldRequest || cmd == nil || w.name.Value() != "My Pack" || len(w.history) != historySize {
			t.Fatal("retry lost wizard state or reused its request")
		}
	}
}

func freshWizard(recipes []*adminv1.RecipeInfo) wizard {
	w := wizard{icons: unicodeIcons(), styles: newStyles(), recipes: recipes, name: textinput.New()}
	w.loaderIndex = map[string][]string{"forge": {"10.13.4.1614", "10.13.4.1558"}, "neoforge": {"21.1.250"}}
	return w
}

func TestWizardBackRestoresThePreviousStepAndName(t *testing.T) {
	w := freshWizard(lwjgl3ify())
	w.pick = newPicker("Версия", []pickItem{{label: "1.7.10", value: "1.7.10"}}, w.icons, w.styles)
	w, _ = w.advance("1.7.10")
	w, _ = w.Update(loadersMsg{request: w.request, minecraftVersion: "1.7.10", loaders: []*adminv1.LoaderInfo{{Name: "forge", Versions: []string{"10.13.4.1614"}}}, recipes: lwjgl3ify()})
	w, _ = w.advance("forge")
	w, _ = w.advance("lwjgl3ify")
	w.name.SetValue("Сборка с пробелами")
	for _, want := range []wizardStep{wzRecipe, wzLoader, wzVersion} {
		w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if w.cancel || w.step != want || w.name.Value() != "Сборка с пробелами" {
			t.Fatalf("back: step=%v cancel=%v name=%q", w.step, w.cancel, w.name.Value())
		}
	}
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !w.cancel {
		t.Fatal("Escape on the first step must cancel")
	}
}

func TestWizardIgnoresLoaderResponseAfterGoingBack(t *testing.T) {
	w := freshWizard(nil)
	w, _ = w.advance("1.21.1")
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyEsc})
	w, _ = w.Update(loadersMsg{minecraftVersion: "1.21.1", loaders: []*adminv1.LoaderInfo{{Name: "vanilla"}}})
	if w.step != wzVersion || w.loading || w.cancel {
		t.Fatal("late response overwrote previous step")
	}
}

func TestWizardCommandsPreserveNames(t *testing.T) {
	w := freshWizard(nil)
	w.mc = "1.21.1"
	w.name.SetValue(`Пакет "один" 'два'`)
	args, err := command.Split(w.buildCommand())
	if err != nil || !reflect.DeepEqual(args, w.buildArguments()) {
		t.Fatalf("wizard arguments changed: %q, %v", args, err)
	}
}

func lwjgl3ify() []*adminv1.RecipeInfo {
	return []*adminv1.RecipeInfo{{Name: "lwjgl3ify", Summary: "1.7.10 на LWJGL 3", Loader: "forge"}}
}

func TestRecipeIsOfferedRightAfterTheLoaderAndSkipsItsVersion(t *testing.T) {
	w := freshWizard(lwjgl3ify())
	w.mc = "1.7.10"
	w.step = wzLoader
	w, _ = w.advance("forge")
	if w.step != wzRecipe {
		t.Fatalf("после загрузчика ждали вопрос про рецепт, а шаг = %v", w.step)
	}

	w, _ = w.advance("lwjgl3ify")
	if w.step != wzName {
		t.Fatalf("после выбора рецепта версия загрузчика не нужна — её несёт рецепт, а шаг = %v", w.step)
	}
	w.name.SetValue("retro")
	command := w.buildCommand()
	if !strings.Contains(command, "compat=lwjgl3ify") {
		t.Fatalf("команда без рецепта: %s", command)
	}
	if strings.Contains(command, "loaderVersion=") {
		t.Fatalf("версия загрузчика попала в команду, хотя рецепт её переопределит: %s", command)
	}
}

func TestDecliningTheRecipeBringsBackTheLoaderVersionQuestion(t *testing.T) {
	w := freshWizard(lwjgl3ify())
	w.mc = "1.7.10"
	w.step = wzLoader
	w, _ = w.advance("forge")
	w, _ = w.advance(recipeDecline)
	if w.step != wzLoaderVersion {
		t.Fatalf("отказ от рецепта должен вернуть выбор версии загрузчика, а шаг = %v", w.step)
	}

	w, _ = w.advance("10.13.4.1614")
	w.name.SetValue("retro")
	command := w.buildCommand()
	if !strings.Contains(command, "compat=нет") {
		t.Fatalf("отказ должен отключить ранее сохранённый рецепт: %s", command)
	}
	if !strings.Contains(command, "loaderVersion=10.13.4.1614") {
		t.Fatalf("версия загрузчика потерялась: %s", command)
	}
}

func TestAVersionWithoutAFittingRecipeIsNeverAsked(t *testing.T) {
	w := freshWizard(lwjgl3ify())
	w.mc = "1.21.1"
	w.step = wzLoader
	w, _ = w.advance("neoforge")
	if w.step != wzLoaderVersion {
		t.Fatalf("рецепт под forge не подходит neoforge — вопроса быть не должно, а шаг = %v", w.step)
	}

	w = freshWizard(nil)
	w.mc = "1.21.1"
	w.step = wzLoader
	w, _ = w.advance("vanilla")
	if w.step != wzName {
		t.Fatalf("у vanilla нет ни рецептов, ни версий загрузчика, а шаг = %v", w.step)
	}
}
