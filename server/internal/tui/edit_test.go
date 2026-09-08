package tui

import "testing"

func TestEditingStartsFromTheRealValueNotThePlaceholder(t *testing.T) {
	nothing := ""
	empty := menuItem{label: "Доверенные прокси", value: "не задано", raw: &nothing}
	if empty.editable() == "не задано" {
		t.Fatal("подпись «не задано» подставляется в строку ввода и уезжает в конфиг")
	}

	stored := "127.0.0.1"
	filled := menuItem{label: "Доверенные прокси", value: "127.0.0.1", raw: &stored}
	if filled.editable() != "127.0.0.1" {
		t.Fatalf("правка должна начинаться с текущего значения, получили %q", filled.editable())
	}
}
