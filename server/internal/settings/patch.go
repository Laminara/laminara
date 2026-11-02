package settings

import (
	"errors"
	"fmt"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/auth/hash"
	"strings"
)

type PatchDocument interface {
	Revision() string
	Set(string, string) error
	Edit(string, string) error
	SetItem(string, string, string, bool) error
	AddEntry(string, string) (string, error)
	RemoveEntry(string) error
	Entry(string) (Entry, error)
	EntryFields(string) ([]Entry, error)
}

func ApplyPatch(doc PatchDocument, patch *adminv1.SettingsPatch, commit bool) ([]*adminv1.SettingDifference, string, error) {
	var err error
	if patch == nil {
		return nil, "", nil
	}
	if len(patch.Changes) > 256 {
		return nil, "", errors.New("слишком много несохранённых изменений; сохраните их частями")
	}
	if patch.Revision != "" && patch.Revision != doc.Revision() {
		return nil, "", ErrChanged
	}
	if len(patch.Changes) > 0 && patch.Revision == "" {
		return nil, "", errors.New("нет исходной ревизии настроек")
	}
	var differences []*adminv1.SettingDifference
	var added string
	for _, change := range patch.Changes {
		if change == nil {
			return nil, "", errors.New("пустое изменение настроек")
		}
		var path, before, after string
		switch action := change.Action.(type) {
		case *adminv1.SettingChange_Set:
			if action.Set == nil {
				return nil, "", errors.New("пустая запись настройки")
			}
			path = action.Set.Path
			before = settingSummary(doc, path)
			switch action.Set.Mode {
			case adminv1.SettingWriteMode_SETTING_WRITE_MODE_PASSWORD:
				if path != "console.password" || action.Set.Value == "" {
					return nil, "", errors.New("пароль консоли не должен быть пустым")
				}
				digest := SecretMask
				if commit {
					digest, err = hash.ProduceCost("argon2id", action.Set.Value, 0)
					if err != nil {
						return nil, "", err
					}
				}
				err = doc.Set(path, digest)
			case adminv1.SettingWriteMode_SETTING_WRITE_MODE_UNSPECIFIED, adminv1.SettingWriteMode_SETTING_WRITE_MODE_REPLACE:
				err = doc.Set(path, action.Set.Value)
			case adminv1.SettingWriteMode_SETTING_WRITE_MODE_EDIT_REDACTED:
				err = doc.Edit(path, action.Set.Value)
			case adminv1.SettingWriteMode_SETTING_WRITE_MODE_CLEAR:
				if action.Set.Value != "" {
					return nil, "", errors.New("очистка не принимает значение")
				}
				err = doc.Set(path, "")
			default:
				err = errors.New("неизвестный режим записи")
			}
			after = settingSummary(doc, path)
		case *adminv1.SettingChange_Add:
			if action.Add == nil {
				return nil, "", errors.New("не указан список")
			}
			path, err = doc.AddEntry(action.Add.Collection, action.Add.Name)
			added, before, after = path, "нет записи", "новая запись"
		case *adminv1.SettingChange_Remove:
			if action.Remove == nil {
				return nil, "", errors.New("не указана удаляемая запись")
			}
			path = action.Remove.Path
			before = settingSummary(doc, path)
			err = doc.RemoveEntry(path)
			after = "удалено"
		case *adminv1.SettingChange_Item:
			if action.Item == nil {
				return nil, "", errors.New("не указан элемент настройки")
			}
			path = action.Item.Path
			before = settingSummary(doc, path)
			err = doc.SetItem(path, action.Item.Key, action.Item.Value, action.Item.Remove)
			after = settingSummary(doc, path)
		default:
			err = errors.New("неизвестное изменение настроек")
		}
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", path, err)
		}
		differences = append(differences, &adminv1.SettingDifference{Path: path, Before: before, After: after})
	}
	return differences, added, nil
}

func settingSummary(doc PatchDocument, path string) string {
	entry, err := doc.Entry(path)
	if err == nil {
		if !entry.IsSet {
			return entry.Display + " (по умолчанию)"
		}
		return entry.Display
	}
	fields, err := doc.EntryFields(path)
	if err != nil {
		return "не задано"
	}
	var words []string
	for _, field := range fields {
		words = append(words, field.Label+": "+field.Display)
	}
	return strings.Join(words, "; ")
}
