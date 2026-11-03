package admin

import (
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/settings"
)

func DescribeSettings(doc *settings.Doc, section, entry, query string) (SettingsPage, error) {
	page := SettingsPage{ConfigPath: doc.Path(), Revision: doc.Revision()}
	var fields []settings.Entry
	var err error
	base := section
	switch {
	case query != "":
		fields = doc.Search(query)
	case entry != "":
		fields, err = doc.EntryFields(entry)
		base = entry
	case section != "":
		fields, err = doc.Section(section)
	default:
		for _, item := range doc.Sections() {
			page.Sections = append(page.Sections, &adminv1.SettingSection{Key: item.Key, Title: item.Title, Hint: item.Hint})
		}
	}
	if err != nil {
		return SettingsPage{}, err
	}
	page.Entries = SettingEntriesProto(fields)
	if base != "" && query == "" {
		collections, err := doc.Collections(base)
		if err != nil {
			return SettingsPage{}, err
		}
		for _, collection := range collections {
			view := &adminv1.SettingCollection{Path: collection.Path, Title: collection.Title, Hint: collection.Hint, Keyed: collection.Keyed, NameLabel: collection.NameLabel, NameHint: collection.NameHint}
			for _, item := range collection.Entries {
				view.Entries = append(view.Entries, &adminv1.SettingCollectionEntry{Path: item.Path, Title: item.Title, Hint: item.Hint})
			}
			page.Collections = append(page.Collections, view)
		}
	}
	return page, nil
}

func SettingEntriesProto(entries []settings.Entry) []*adminv1.SettingEntry {
	result := make([]*adminv1.SettingEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, &adminv1.SettingEntry{Path: entry.Path, Label: entry.Label, Hint: entry.Hint, Kind: string(entry.Kind), Value: entry.Value, DefaultValue: entry.Default, IsSet: entry.IsSet, Options: entry.Options, Display: entry.Display, Group: entry.Group, Required: entry.Required, Min: entry.Min, Max: entry.Max, AllowZero: entry.AllowZero})
	}
	return result
}
