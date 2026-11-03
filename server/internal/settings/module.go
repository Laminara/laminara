package settings

import (
	"encoding/json"
	"errors"
	"sync"
)

var moduleCatalog = struct {
	sync.RWMutex
	entries map[string]*CollectionEntryView
}{entries: map[string]*CollectionEntryView{}}

func TrackModule(name string) (func(string, string), func()) {
	entry := &CollectionEntryView{Title: name, Hint: "Модуль не загрузился. Проверьте журнал сервера."}
	moduleCatalog.Lock()
	moduleCatalog.entries[name] = entry
	moduleCatalog.Unlock()
	update := func(title, hint string) {
		moduleCatalog.Lock()
		entry.Title, entry.Hint = title, hint
		moduleCatalog.Unlock()
	}
	remove := func() {
		moduleCatalog.Lock()
		if moduleCatalog.entries[name] == entry {
			delete(moduleCatalog.entries, name)
		}
		moduleCatalog.Unlock()
	}
	return update, remove
}

func discoveredModules() map[string]CollectionEntryView {
	moduleCatalog.RLock()
	defer moduleCatalog.RUnlock()
	entries := make(map[string]CollectionEntryView, len(moduleCatalog.entries))
	for name, entry := range moduleCatalog.entries {
		entries[name] = *entry
	}
	return entries
}

func PrepareModuleConfig(name string, data []byte, defaults bool) ([]byte, error) {
	if len(data) == 0 {
		data = []byte("{}")
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil || config == nil {
		return nil, errors.New("настройки модуля должны быть JSON-объектом")
	}
	extensions.RLock()
	fields := append([]Field(nil), extensions.fields["module"][name]...)
	extensions.RUnlock()
	doc := &Doc{
		tree: map[string]any{"modules": map[string]any{"config": map[string]any{name: config}}},
		schema: []Section{{
			Key:         "modules",
			Collections: []Collection{{Key: "config", Keyed: true, Fields: fields}},
		}},
	}
	base := "modules.config." + pathKey(name)
	if defaults {
		for _, field := range fields {
			path := base + "." + field.Key
			if _, exists := doc.raw(path); exists || field.Default == "" {
				continue
			}
			value, err := parse(field, field.Default)
			if err != nil {
				return nil, err
			}
			if err := doc.put(path, value); err != nil {
				return nil, err
			}
		}
	}
	if err := doc.validateExtensions(); err != nil {
		return nil, err
	}
	return json.Marshal(config)
}

func cloneBound(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
