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
	var collections []Collection
	if section, ok := d.sectionOf(sectionKey); ok {
		collections = section.Collections
	} else if collection, _, ok := d.collectionAt(sectionKey); ok {
		collections = collection.children()
	} else {
		return nil, fmt.Errorf("нет раздела или записи %s", sectionKey)
	}
	views := make([]CollectionView, 0, len(collections))
	for _, collection := range collections {
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
			entries = append(entries, d.entryOf(entryPath, field, ""))
			continue
		}
		if field.Variants == nil {
			entries = append(entries, d.entryOf(entryPath, field, field.Key))
			continue
		}
		chosen := d.stringAt(entryPath+"."+field.VariantOf, fieldDefault(collection.Fields, field.VariantOf))
		if _, known := field.Variants[chosen]; !known {
			entries = append(entries, d.entryOf(entryPath, field, field.Key))
			continue
		}
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
	sectionKey, _, _ := strings.Cut(entryPath, ".")
	section, ok := d.sectionOf(sectionKey)
	if !ok {
		return Collection{}, "", false
	}
	return entryCollection(sectionKey, section.Collections, entryPath)
}

func (c Collection) children() []Collection {
	if c.Children == nil {
		return nil
	}
	return c.Children()
}

func entryCollection(base string, collections []Collection, path string) (Collection, string, bool) {
	for _, collection := range collections {
		prefix := base + "." + collection.Key + "."
		tail, ok := strings.CutPrefix(path, prefix)
		if !ok {
			continue
		}
		id, nested, hasNested := strings.Cut(tail, ".")
		if id == "" {
			return Collection{}, "", false
		}
		if !hasNested {
			return collection.forEntry(id), id, true
		}
		return entryCollection(prefix+id, collection.children(), prefix+id+"."+nested)
	}
	return Collection{}, "", false
}

func (c Collection) forEntry(id string) Collection {
	if c.EntryFields != nil {
		if fields := c.EntryFields(id); len(fields) > 0 {
			c.Fields = fields
		}
	}
	return c
}

func (d *Doc) findCollection(path string) (Collection, bool) {
	sectionKey, _, _ := strings.Cut(path, ".")
	section, ok := d.sectionOf(sectionKey)
	if !ok {
		return Collection{}, false
	}
	var find func(string, []Collection) (Collection, bool)
	find = func(base string, collections []Collection) (Collection, bool) {
		for _, collection := range collections {
			prefix := base + "." + collection.Key
			if path == prefix {
				return collection, true
			}
			if rest, ok := strings.CutPrefix(path, prefix+"."); ok {
				id, _, nested := strings.Cut(rest, ".")
				if nested {
					return find(prefix+"."+id, collection.children())
				}
			}
		}
		return Collection{}, false
	}
	return find(sectionKey, section.Collections)
}

func (d *Doc) AddEntry(collectionPath, name string) (string, error) {
	collection, ok := d.findCollection(collectionPath)
	if !ok {
		return "", fmt.Errorf("нет списка %s", collectionPath)
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
		initial := field.Initial
		if initial == "" {
			initial = field.Default
		}
		if initial == "" {
			continue
		}
		if value, err := parse(field, initial); err == nil {
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
