package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
)

func freshWizard(recipes []*adminv1.RecipeInfo) wizard {
	w := wizard{icons: unicodeIcons(), styles: newStyles(), recipes: recipes, name: textinput.New()}
	w.loaderIndex = map[string][]string{"forge": {"10.13.4.1614", "10.13.4.1558"}, "neoforge": {"21.1.250"}}
	return w
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
	if strings.Contains(command, "compat=") {
		t.Fatalf("рецепт отклонён, но попал в команду: %s", command)
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
