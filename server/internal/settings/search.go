package settings

import "strings"

func (d *Doc) Search(query string) []Entry {
	query = strings.ToLower(strings.TrimSpace(query))
	var result []Entry
	add := func(entries []Entry) {
		for _, entry := range entries {
			if strings.Contains(strings.ToLower(entry.Path+" "+entry.Label+" "+entry.Hint), query) && len(result) < 200 {
				entry.Hint = entry.Path + " · " + entry.Hint
				result = append(result, entry)
			}
		}
	}
	var descend func(string)
	descend = func(path string) {
		if len(result) >= 200 {
			return
		}
		collections, err := d.Collections(path)
		if err != nil {
			return
		}
		for _, collection := range collections {
			for _, item := range collection.Entries {
				entries, err := d.EntryFields(item.Path)
				if err == nil {
					add(entries)
				}
				descend(item.Path)
			}
		}
	}
	for _, section := range d.Sections() {
		entries, err := d.Section(section.Key)
		if err == nil {
			add(entries)
		}
		descend(section.Key)
	}
	return result
}
