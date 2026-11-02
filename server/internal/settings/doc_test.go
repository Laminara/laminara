package settings

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSaveRefusesAnExternalChange(t *testing.T) {
	path := write(t, `{}`)
	doc, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("branding.name", "Mine"); err != nil {
		t.Fatal(err)
	}
	external := `{"branding":{"name":"Someone else"}}`
	if err := os.WriteFile(path, []byte(external), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(); !errors.Is(err, ErrChanged) {
		t.Fatalf("expected revision conflict, got %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != external {
		t.Fatal("external edit overwritten")
	}
}

func TestRevisionChangesOnlyAfterSave(t *testing.T) {
	doc, err := Open(write(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	before := doc.Revision()
	if err := doc.Set("branding.name", "Mine"); err != nil {
		t.Fatal(err)
	}
	if doc.Revision() != before {
		t.Fatal("in-memory change replaced source revision")
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	if doc.Revision() == before {
		t.Fatal("saved revision unchanged")
	}
	data, _ := os.ReadFile(doc.Path())
	if !strings.Contains(string(data), "Mine") {
		t.Fatal("save lost value")
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
}
