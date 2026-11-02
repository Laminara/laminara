package settings

import (
	"strings"
	"testing"
)

func TestModuleSchemaRejectsOverlappingFields(t *testing.T) {
	for _, keys := range [][]string{{"server", "server.token"}, {"server.token", "server"}} {
		remove, err := RegisterSchema("module", "overlap", []SchemaField{{Key: keys[0], Kind: KindText}, {Key: keys[1], Kind: KindSecret}})
		if err == nil {
			remove()
			t.Fatalf("overlapping fields accepted: %v", keys)
		}
	}
}

func TestModuleSchemaExposesFieldsAndValidatesWithoutShowingUnknownSecrets(t *testing.T) {
	remove, err := RegisterSchema("module", "demo", []SchemaField{{Key: "endpoint", Kind: KindText, Label: "Адрес", Required: true}, {Key: "token", Kind: KindSecret}, {Key: "enabled", Kind: KindBool}})
	if err != nil {
		t.Fatal(err)
	}
	defer remove()
	doc, err := Open(write(t, `{"modules":{"config":{"demo":{"endpoint":"https://example.test","token":"SECRET","unexpected":"PRIVATE"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	fields, err := doc.EntryFields("modules.config.demo")
	if err != nil || len(fields) != 3 {
		t.Fatalf("fields: %v %v", fields, err)
	}
	if fields[0].Value != "https://example.test" || fields[1].Value != SecretMask {
		t.Fatalf("typed values: %+v", fields)
	}
	whole, err := doc.Entry("modules.config.demo")
	if err != nil || strings.Contains(whole.Value, "PRIVATE") || strings.Contains(whole.Value, "SECRET") {
		t.Fatalf("whole JSON exposed: %v %v", whole, err)
	}
	if err := doc.Set("modules.config.demo.enabled", "да"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("modules.config.demo.endpoint", ""); err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(); err == nil {
		t.Fatal("required field cleared")
	}
}

func TestAdapterSchemaIsUsedByTheCommonEditor(t *testing.T) {
	remove, err := RegisterSchema("auth", "custom", []SchemaField{{Key: "url", Kind: KindText}, {Key: "secret", Kind: KindSecret}})
	if err != nil {
		t.Fatal(err)
	}
	defer remove()
	doc, err := Open(write(t, `{"auth":{"provider":"custom","config":{"url":"example","secret":"SECRET","opaque":"PRIVATE"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := doc.Entry("auth.config.url")
	if err != nil || entry.Value != "example" {
		t.Fatalf("adapter field: %v %v", entry, err)
	}
	entry, err = doc.Entry("auth.config")
	if err != nil || strings.Contains(entry.Value, "PRIVATE") {
		t.Fatalf("opaque field leaked: %v %v", entry, err)
	}
}
