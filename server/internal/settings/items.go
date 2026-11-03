package settings

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

func (d *Doc) SetItem(path, key, value string, remove bool) error {
	_, field, _, err := d.resolve(path)
	if err != nil {
		return err
	}
	raw, _ := d.raw(path)
	switch field.Kind {
	case KindPairs, KindMap:
		if strings.TrimSpace(key) == "" {
			return errors.New("введите непустой ключ")
		}
		if field.Kind == KindPairs && strings.ContainsAny(key, "\r\n: \t") {
			return errors.New("введите имя заголовка без пробелов, двоеточия и перевода строки")
		}
		if field.Kind == KindPairs && strings.ContainsAny(value, "\r\n") {
			return errors.New("значение заголовка не должно содержать перевод строки")
		}
		previous, _ := raw.(map[string]any)
		if raw == nil && field.Default != "" {
			initial, err := parse(field, field.Default)
			if err != nil {
				return err
			}
			previous, _ = initial.(map[string]any)
		}
		pairs := maps.Clone(previous)
		if pairs == nil {
			pairs = map[string]any{}
		}
		for oldKey := range pairs {
			if field.Kind == KindPairs && strings.EqualFold(oldKey, key) && oldKey != key {
				return errors.New("заголовок с таким именем уже есть; откройте существующий")
			}
		}
		if remove {
			delete(pairs, key)
		} else {
			pairs[key] = value
		}
		return d.put(path, pairs)
	case KindList:
		if raw == nil {
			entry, err := d.Entry(path)
			if err != nil {
				return err
			}
			if entry.Value != "" {
				raw, err = parse(field, entry.Value)
				if err != nil {
					return err
				}
			}
		}
		previous, _ := raw.([]any)
		entries := slices.Clone(previous)
		if key == "" && !remove {
			return d.put(path, append(entries, value))
		}
		index, err := strconv.Atoi(key)
		if err != nil || index < 0 || index >= len(entries) {
			return fmt.Errorf("элемент списка больше не существует")
		}
		if remove {
			entries = slices.Delete(entries, index, index+1)
		} else {
			entries[index] = value
		}
		return d.put(path, entries)
	default:
		return errors.New("эта настройка не является списком или заголовками")
	}
}
