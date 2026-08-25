package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/admin"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/settings"
)

type settingsStore struct {
	path    string
	restart func() error

	mu      sync.Mutex
	pending bool
	changed func(path string)
}

func (s *settingsStore) List(section, entry string) (admin.SettingsPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := settings.Open(s.path)
	if err != nil {
		return admin.SettingsPage{}, err
	}
	page := admin.SettingsPage{ConfigPath: s.path, Pending: s.pending}
	switch {
	case entry != "":
		fields, err := doc.EntryFields(entry)
		if err != nil {
			return admin.SettingsPage{}, err
		}
		page.Entries = entriesToProto(fields)
	case section != "":
		fields, err := doc.Section(section)
		if err != nil {
			return admin.SettingsPage{}, err
		}
		page.Entries = entriesToProto(fields)
		collections, err := doc.Collections(section)
		if err != nil {
			return admin.SettingsPage{}, err
		}
		for _, collection := range collections {
			view := &adminv1.SettingCollection{
				Path:      collection.Path,
				Title:     collection.Title,
				Hint:      collection.Hint,
				Keyed:     collection.Keyed,
				NameLabel: collection.NameLabel,
				NameHint:  collection.NameHint,
			}
			for _, item := range collection.Entries {
				view.Entries = append(view.Entries, &adminv1.SettingCollectionEntry{Path: item.Path, Title: item.Title})
			}
			page.Collections = append(page.Collections, view)
		}
	default:
		for _, item := range settings.Sections() {
			page.Sections = append(page.Sections, &adminv1.SettingSection{Key: item.Key, Title: item.Title, Hint: item.Hint})
		}
	}
	return page, nil
}

func entriesToProto(entries []settings.Entry) []*adminv1.SettingEntry {
	out := make([]*adminv1.SettingEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, &adminv1.SettingEntry{
			Path:         entry.Path,
			Label:        entry.Label,
			Hint:         entry.Hint,
			Kind:         string(entry.Kind),
			Value:        entry.Value,
			DefaultValue: entry.Default,
			IsSet:        entry.IsSet,
			Options:      entry.Options,
			Display:      entry.Display,
		})
	}
	return out
}

func (s *settingsStore) One(path string) (*adminv1.SettingEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := settings.Open(s.path)
	if err != nil {
		return nil, err
	}
	entry, err := doc.Entry(path)
	if err != nil {
		return nil, err
	}
	return entriesToProto([]settings.Entry{entry})[0], nil
}

func (s *settingsStore) Set(path, value string) (*adminv1.SettingEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := settings.Open(s.path)
	if err != nil {
		return nil, err
	}
	if err := doc.Set(path, value); err != nil {
		return nil, err
	}
	if err := doc.Save(); err != nil {
		return nil, err
	}
	s.pending = true
	if s.changed != nil {
		s.changed(path)
	}
	entry, err := doc.Entry(path)
	if err != nil {
		return nil, err
	}
	return entriesToProto([]settings.Entry{entry})[0], nil
}

func (s *settingsStore) Add(collection, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := settings.Open(s.path)
	if err != nil {
		return "", err
	}
	path, err := doc.AddEntry(collection, name)
	if err != nil {
		return "", err
	}
	if err := doc.Save(); err != nil {
		return "", err
	}
	s.pending = true
	if s.changed != nil {
		s.changed(path)
	}
	return path, nil
}

func (s *settingsStore) Remove(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := settings.Open(s.path)
	if err != nil {
		return err
	}
	if err := doc.RemoveEntry(path); err != nil {
		return err
	}
	if err := doc.Save(); err != nil {
		return err
	}
	s.pending = true
	if s.changed != nil {
		s.changed(path)
	}
	return nil
}

func (s *settingsStore) Restart() error {
	if s.restart == nil {
		return errors.New("этот сервер сам себя перезапустить не может")
	}
	return s.restart()
}

func settingsCommand(store *settingsStore) command.Command {
	return command.Command{
		Name:     "settings",
		Aliases:  []string{"config"},
		Synopsis: "настройки проекта (settings | settings <раздел> | settings <путь> [значение] | settings clear <путь> | settings add <список> [имя] | settings remove <путь>)",
		Run: func(_ context.Context, args []string, out io.Writer) error {
			if store == nil {
				return errors.New("сервер запущен без файла настроек — править нечего")
			}
			switch {
			case len(args) == 0:
				page, err := store.List("", "")
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Настройки лежат в %s\n\n", page.ConfigPath)
				for _, section := range page.Sections {
					fmt.Fprintf(out, "%-12s %s\n", section.Key, section.Title)
				}
				fmt.Fprintln(out, "\nПодробнее: settings <раздел>")
				return nil
			case args[0] == "add":
				if len(args) < 2 {
					return errors.New("напишите список: settings add access.rules [имя]")
				}
				name := ""
				if len(args) > 2 {
					name = args[2]
				}
				path, err := store.Add(args[1], name)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Добавлено: %s\n", path)
				return nil
			case args[0] == "clear" || args[0] == "reset":
				if len(args) < 2 {
					return errors.New("напишите путь: settings clear branding.tagline")
				}
				entry, err := store.Set(args[1], "")
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s: %s\n", entry.Label, valueWord(entry))
				return nil
			case args[0] == "remove" || args[0] == "delete":
				if len(args) < 2 {
					return errors.New("напишите путь: settings remove access.rules.0")
				}
				if err := store.Remove(args[1]); err != nil {
					return err
				}
				fmt.Fprintf(out, "Удалено: %s\n", args[1])
				return nil
			case !strings.Contains(args[0], "."):
				return printSection(store, args[0], out)
			case len(args) == 1:
				return printEntry(store, args[0], out)
			default:
				entry, err := store.Set(args[0], strings.Join(args[1:], " "))
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s: %s\n", entry.Label, valueWord(entry))
				fmt.Fprintln(out, "Записано. Чтобы настройка заработала, перезапустите проект командой restart.")
				return nil
			}
		},
	}
}

func printSection(store *settingsStore, section string, out io.Writer) error {
	page, err := store.List(section, "")
	if err != nil {
		return err
	}
	width := 0
	for _, entry := range page.Entries {
		width = max(width, len([]rune(entry.Label)))
	}
	for _, entry := range page.Entries {
		fmt.Fprintf(out, "%-*s  %-28s %s\n", width, entry.Label, valueWord(entry), entry.Path)
	}
	for _, collection := range page.Collections {
		fmt.Fprintf(out, "\n%s:\n", collection.Title)
		if len(collection.Entries) == 0 {
			fmt.Fprintln(out, "  пусто")
		}
		for _, item := range collection.Entries {
			fmt.Fprintf(out, "  %-28s %s\n", item.Title, item.Path)
		}
	}
	return nil
}

func printEntry(store *settingsStore, path string, out io.Writer) error {
	if page, err := store.List("", path); err == nil && len(page.Entries) > 0 {
		for _, entry := range page.Entries {
			fmt.Fprintf(out, "%-28s %-24s %s\n", entry.Label, valueWord(entry), entry.Path)
		}
		return nil
	}
	entry, err := store.One(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s\n", entry.Label)
	fmt.Fprintf(out, "  значение:     %s\n", valueWord(entry))
	if entry.DefaultValue != "" {
		fmt.Fprintf(out, "  по умолчанию: %s\n", entry.DefaultValue)
	}
	if len(entry.Options) > 0 {
		fmt.Fprintf(out, "  варианты:     %s\n", strings.Join(entry.Options, ", "))
	}
	if entry.Hint != "" {
		fmt.Fprintf(out, "  %s\n", entry.Hint)
	}
	fmt.Fprintf(out, "  изменить:     settings %s <значение>\n", path)
	return nil
}

func valueWord(entry *adminv1.SettingEntry) string {
	shown := entry.Display
	if shown == "" {
		shown = entry.Value
	}
	if shown == "" {
		return "не задано"
	}
	if !entry.IsSet {
		return shown + " (по умолчанию)"
	}
	return shown
}

func restartCommand(store *settingsStore) command.Command {
	return command.Command{
		Name:     "restart",
		Synopsis: "перезапустить проект, чтобы новые настройки заработали",
		Run: func(_ context.Context, _ []string, out io.Writer) error {
			if store == nil {
				return errors.New("этот сервер сам себя перезапустить не может")
			}
			fmt.Fprintln(out, "Перезапускаю проект…")
			return store.Restart()
		},
	}
}
