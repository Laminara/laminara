package settings

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDiscoveredModuleCanBeConfiguredWithoutStoredEntry(t *testing.T) {
	name := "demo.plugin%2E"
	update, untrack := TrackModule(name)
	defer untrack()
	update("Демо", "Требует настройки")
	remove, err := RegisterSchema("module", name, []SchemaField{{Key: "token", Kind: KindSecret, Required: true}, {Key: "connection.timeout", Kind: KindDuration, Default: "10s"}})
	if err != nil {
		t.Fatal(err)
	}
	defer remove()
	doc, err := Open(write(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	collections, err := doc.Collections("modules")
	if err != nil || len(collections[0].Entries) != 1 {
		t.Fatalf("inventory: %v %v", collections, err)
	}
	entry := collections[0].Entries[0]
	if entry.Title != "Демо" || entry.Hint != "Требует настройки" {
		t.Fatalf("metadata: %+v", entry)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("unconfigured module blocked unrelated settings: %v", err)
	}
	fields, err := doc.EntryFields(entry.Path)
	if err != nil || len(fields) != 2 || fields[1].Value != "10s" {
		t.Fatalf("fields: %v %v", fields, err)
	}
	if err := doc.Set(entry.Path+".token", "PRIVATE"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	stored, err := PrepareModuleConfig(name, []byte(`{"token":"PRIVATE","unknown":17}`), true)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	if err := json.Unmarshal(stored, &values); err != nil {
		t.Fatal(err)
	}
	if values["unknown"] != float64(17) || values["connection"].(map[string]any)["timeout"] != "10s" {
		t.Fatalf("runtime config: %s", stored)
	}
	shown, err := doc.Entry(entry.Path + ".token")
	if err != nil || shown.Value != SecretMask {
		t.Fatalf("secret: %+v %v", shown, err)
	}
	data, _ := doc.Bytes()
	if !strings.Contains(string(data), `"demo.plugin%2E"`) || strings.Contains(string(data), `"demo":`) {
		t.Fatalf("wrong disk key: %s", data)
	}
	if err := doc.RemoveEntry(entry.Path); err != nil {
		t.Fatal(err)
	}
	if _, exists := doc.raw(entry.Path); exists {
		t.Fatal("config was not removed")
	}
}

func TestModuleDefaultsConstraintsAndMap(t *testing.T) {
	min, max := int64(1), int64(5)
	remove, err := RegisterSchema("module", "123", []SchemaField{
		{Key: "workers", Kind: KindInt, Default: "2", Min: &min, Max: &max, Group: "Работа"},
		{Key: "labels", Kind: KindMap, Default: `{"Region":"one","region":"two"}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer remove()
	doc, err := Open(write(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("modules.config.123.workers", "3"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("modules.config.123.workers", "6"); err == nil {
		t.Fatal("range ignored")
	}
	if err := doc.SetItem("modules.config.123.labels", "region", "three", false); err != nil {
		t.Fatal(err)
	}
	labels, _ := doc.raw("modules.config.123.labels")
	if labels.(map[string]any)["Region"] != "one" || labels.(map[string]any)["region"] != "three" {
		t.Fatalf("map keys/defaults lost: %v", labels)
	}
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{`{"workers":0}`, `{"workers":6}`, `{"workers":1.5}`, `{"workers":"2"}`, `null`, `[]`} {
		t.Run(input, func(t *testing.T) {
			if _, err := PrepareModuleConfig("123", []byte(input), true); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	configured, err := PrepareModuleConfig("123", []byte(`{}`), true)
	if err != nil || !strings.Contains(string(configured), `"workers":2`) {
		t.Fatalf("defaults: %s %v", configured, err)
	}
	legacy, err := PrepareModuleConfig("123", []byte(`{}`), false)
	if err != nil || string(legacy) != `{}` {
		t.Fatalf("legacy defaults changed: %s %v", legacy, err)
	}
}

func TestModuleCompoundDefaultsKeepExactValues(t *testing.T) {
	remove, err := RegisterSchema("module", "compound-defaults", []SchemaField{
		{Key: "names", Kind: KindList, Default: `["one, two"," three "]`},
		{Key: "headers", Kind: KindPairs, Default: `{"Accept":"application/json"}`},
		{Key: "labels", Kind: KindMap},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer remove()
	doc, err := Open(write(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"names": `["one, two"," three "]`, "headers": `{"Accept":"application/json"}`} {
		entry, err := doc.Entry("modules.config.compound-defaults." + key)
		if err != nil || entry.Value != want {
			t.Errorf("%s: %q, want %q, error %v", key, entry.Value, want, err)
		}
	}
	if err := doc.SetItem("modules.config.compound-defaults.names", "1", "changed", false); err != nil {
		t.Fatal(err)
	}
	entry, err := doc.Entry("modules.config.compound-defaults.names")
	if err != nil || entry.Value != `["one, two","changed"]` {
		t.Fatalf("list default was split: %s %v", entry.Value, err)
	}
	for _, input := range []string{`{"names":3}`, `{"names":[null]}`, `{"labels":{"key":null}}`} {
		if _, err := PrepareModuleConfig("compound-defaults", []byte(input), true); err == nil {
			t.Errorf("invalid compound value accepted: %s", input)
		}
	}
}

func TestModuleRequiredDefaultDoesNotOverrideExplicitEmptyValue(t *testing.T) {
	remove, err := RegisterSchema("module", "required-default", []SchemaField{
		{Key: "name", Kind: KindText, Required: true, Default: "Guest"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer remove()
	for _, input := range []string{`{"name":""}`, `{"name":null}`} {
		if _, err := PrepareModuleConfig("required-default", []byte(input), true); err == nil {
			t.Errorf("empty required value accepted: %s", input)
		}
	}
	configured, err := PrepareModuleConfig("required-default", []byte(`{}`), true)
	if err != nil || string(configured) != `{"name":"Guest"}` {
		t.Fatalf("default: %s %v", configured, err)
	}
}
