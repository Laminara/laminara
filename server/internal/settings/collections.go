package settings

import (
	"fmt"
	"strconv"
	"strings"
)

type CollectionView struct {
	Path      string
	Title     string
	Hint      string
	Keyed     bool
	NameLabel string
	NameHint  string
	Entries   []CollectionEntryView
}

type CollectionEntryView struct {
	Path  string
	Title string
}

func (d *Doc) Collections(sectionKey string) ([]CollectionView, error) {
	section, ok := sectionOf(sectionKey)
	if !ok {
		return nil, fmt.Errorf("раздела «%s» нет", sectionKey)
	}
	views := make([]CollectionView, 0, len(section.Collections))
	for _, collection := range section.Collections {
		path := sectionKey + "." + collection.Key
		view := CollectionView{
			Path:      path,
			Title:     collection.Title,
			Hint:      collection.Hint,
			Keyed:     collection.Keyed,
			NameLabel: collection.NameLabel,
			NameHint:  collection.NameHint,
		}
		for _, key := range d.Keys(path) {
			view.Entries = append(view.Entries, CollectionEntryView{
				Path:  path + "." + key,
				Title: d.entryTitle(collection, path+"."+key, key),
			})
		}
		views = append(views, view)
	}
	return views, nil
}

func (d *Doc) entryTitle(collection Collection, path, key string) string {
	if collection.Keyed {
		return key
	}
	if len(collection.Fields) > 0 {
		if raw, ok := d.raw(path + "." + collection.Fields[0].Key); ok {
			if text := render(collection.Fields[0].Kind, raw); text != "" {
				return text
			}
		}
	}
	if index, err := strconv.Atoi(key); err == nil {
		return "правило " + strconv.Itoa(index+1)
	}
	return key
}

func (d *Doc) EntryFields(entryPath string) ([]Entry, error) {
	collection, _, ok := d.collectionAt(entryPath)
	if !ok {
		return nil, fmt.Errorf("нет записи %s", entryPath)
	}
	var entries []Entry
	for _, field := range collection.Fields {
		if field.Key == "" {
			entries = append(entries, d.entryOf(entryPath, Field{Key: "", Label: field.Label, Hint: field.Hint, Kind: KindJSON}, ""))
			continue
		}
		if field.Variants == nil {
			entries = append(entries, d.entryOf(entryPath, field, field.Key))
			continue
		}
		chosen := d.stringAt(entryPath+"."+field.VariantOf, fieldDefault(collection.Fields, field.VariantOf))
		for _, nested := range field.Variants[chosen] {
			entries = append(entries, d.entryOf(entryPath, nested, field.Key+"."+nested.Key))
		}
	}
	return entries, nil
}

func fieldDefault(fields []Field, key string) string {
	for _, field := range fields {
		if field.Key == key {
			return field.Default
		}
	}
	return ""
}

func (d *Doc) collectionAt(entryPath string) (Collection, string, bool) {
	parts := strings.Split(entryPath, ".")
	if len(parts) < 3 {
		return Collection{}, "", false
	}
	section, ok := sectionOf(parts[0])
	if !ok {
		return Collection{}, "", false
	}
	collection, ok := collectionOf(section, parts[1])
	if !ok {
		return Collection{}, "", false
	}
	return collection, parts[2], true
}

func (d *Doc) AddEntry(collectionPath, name string) (string, error) {
	sectionKey, collectionKey, ok := strings.Cut(collectionPath, ".")
	if !ok {
		return "", fmt.Errorf("нет списка %s", collectionPath)
	}
	section, ok := sectionOf(sectionKey)
	if !ok {
		return "", fmt.Errorf("раздела «%s» нет", sectionKey)
	}
	collection, ok := collectionOf(section, collectionKey)
	if !ok {
		return "", fmt.Errorf("в разделе «%s» нет списка «%s»", section.Title, collectionKey)
	}
	if collection.Keyed {
		name = strings.TrimSpace(name)
		if name == "" {
			return "", fmt.Errorf("нужно имя")
		}
		if strings.ContainsAny(name, ". ") {
			return "", fmt.Errorf("имя без точек и пробелов: %s", name)
		}
		if _, exists := d.raw(collectionPath + "." + name); exists {
			return "", fmt.Errorf("«%s» уже есть", name)
		}
		if err := d.put(collectionPath+"."+name, blank(collection)); err != nil {
			return "", err
		}
		return collectionPath + "." + name, nil
	}
	existing, _ := d.raw(collectionPath)
	list, _ := existing.([]any)
	list = append(list, blank(collection))
	if err := d.put(collectionPath, list); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.%d", collectionPath, len(list)-1), nil
}

func blank(collection Collection) any {
	entry := map[string]any{}
	for _, field := range collection.Fields {
		if field.Key == "" {
			return map[string]any{}
		}
		if field.Default == "" {
			continue
		}
		if value, err := parse(field, field.Default); err == nil {
			assign(entry, field.Key, value)
		}
	}
	return entry
}

func assign(entry map[string]any, key string, value any) {
	steps := strings.Split(key, ".")
	current := entry
	for _, step := range steps[:len(steps)-1] {
		next, ok := current[step].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[step] = next
		}
		current = next
	}
	current[steps[len(steps)-1]] = value
}

func (d *Doc) RemoveEntry(entryPath string) error {
	if _, _, ok := d.collectionAt(entryPath); !ok {
		return fmt.Errorf("нет записи %s", entryPath)
	}
	return d.drop(entryPath)
}
