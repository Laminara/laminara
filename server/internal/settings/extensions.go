package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode"

	sdkmodule "github.com/laminara/laminara/sdk/go/module"
)

type SchemaField = sdkmodule.SettingField

var extensions = struct {
	sync.RWMutex
	fields map[string]map[string][]Field
}{fields: map[string]map[string][]Field{}}

func RegisterSchema(kind, name string, descriptors []SchemaField) (func(), error) {
	if !slices.Contains([]string{"module", "auth", "skin", "access", "news"}, kind) || name == "" || len(descriptors) == 0 || len(descriptors) > 128 {
		return nil, errors.New("неверная схема настроек расширения")
	}
	fields := make([]Field, 0, len(descriptors))
	seen := map[string]bool{}
	for _, descriptor := range descriptors {
		key := descriptor.Key
		if key == "" || strings.ContainsAny(key, " /\\%\x00") || strings.ContainsFunc(key, unicode.IsControl) || strings.HasPrefix(key, ".") || strings.HasSuffix(key, ".") || strings.Contains(key, "..") || seen[key] {
			return nil, errors.New("неверный ключ поля схемы")
		}
		if !slices.Contains([]Kind{KindText, KindSecret, KindBool, KindInt, KindDuration, KindChoice, KindList, KindPairs, KindMap, KindJSON, KindSecretJSON}, descriptor.Kind) {
			return nil, errors.New("неподдерживаемый тип поля схемы")
		}
		for previous := range seen {
			if strings.HasPrefix(key, previous+".") || strings.HasPrefix(previous, key+".") {
				return nil, errors.New("поля схемы пересекаются")
			}
		}
		seen[key] = true
		if (descriptor.Min != nil || descriptor.Max != nil) && descriptor.Kind != KindInt {
			return nil, errors.New("границы поддерживаются только для целых чисел")
		}
		if descriptor.Min != nil && descriptor.Max != nil && *descriptor.Min > *descriptor.Max {
			return nil, errors.New("нижняя граница больше верхней")
		}
		if descriptor.Kind == KindChoice && len(descriptor.Options) == 0 {
			return nil, errors.New("у выбора должен быть хотя бы один вариант")
		}
		if (descriptor.Kind == KindSecret || descriptor.Kind == KindSecretJSON) && descriptor.Default != "" {
			return nil, errors.New("секрет не должен иметь значение по умолчанию в схеме")
		}
		choices := slices.Clone(descriptor.Options)
		field := Field{Key: key, Label: descriptor.Label, Hint: descriptor.Hint, Kind: descriptor.Kind, Default: descriptor.Default, Required: descriptor.Required, AllowZero: descriptor.AllowZero, Extension: true}
		field.Group, field.Min, field.Max = descriptor.Group, cloneBound(descriptor.Min), cloneBound(descriptor.Max)
		if field.Label == "" {
			field.Label = key
		}
		if len(choices) > 0 {
			field.Options = func() []string { return slices.Clone(choices) }
		}
		if field.Default != "" {
			if _, err := parse(field, field.Default); err != nil {
				return nil, fmt.Errorf("неверный default поля %s", key)
			}
		}
		fields = append(fields, field)
	}
	extensions.Lock()
	if extensions.fields[kind] == nil {
		extensions.fields[kind] = map[string][]Field{}
	}
	if _, exists := extensions.fields[kind][name]; exists {
		extensions.Unlock()
		return nil, errors.New("схема уже зарегистрирована")
	}
	extensions.fields[kind][name] = fields
	extensions.Unlock()
	return func() { extensions.Lock(); delete(extensions.fields[kind], name); extensions.Unlock() }, nil
}

func extendedSections(source []Section) []Section {
	extensions.RLock()
	defer extensions.RUnlock()
	var expandFields func(string, []Field) []Field
	expandFields = func(kind string, source []Field) []Field {
		result := slices.Clone(source)
		for i, field := range result {
			if field.Variants == nil {
				continue
			}
			variants := map[string][]Field{}
			for name, nested := range field.Variants {
				variants[name] = expandFields(kind, nested)
			}
			for name, fields := range extensions.fields[kind] {
				if _, exists := variants[name]; !exists {
					variants[name] = fields
				}
			}
			result[i].Variants = variants
		}
		return result
	}
	var expandCollections func(string, []Collection) []Collection
	expandCollections = func(kind string, source []Collection) []Collection {
		result := slices.Clone(source)
		for i, collection := range result {
			result[i].Fields = expandFields(kind, collection.Fields)
			if kind == "modules" && collection.Key == "config" {
				result[i].Discover = discoveredModules
				result[i].EntryFields = func(name string) []Field {
					extensions.RLock()
					defer extensions.RUnlock()
					return slices.Clone(extensions.fields["module"][name])
				}
			}
		}
		return result
	}
	result := slices.Clone(source)
	for i, section := range result {
		kind := section.Key
		if kind == "yggdrasil" {
			kind = "skin"
		}
		result[i].Fields = expandFields(kind, section.Fields)
		result[i].Collections = expandCollections(kind, section.Collections)
	}
	return result
}

func (d *Doc) validateExtensions() error {
	check := func(entries []Entry) error {
		for _, entry := range entries {
			_, field, _, err := d.resolve(entry.Path)
			if err != nil || !field.Extension {
				continue
			}
			value, exists := d.raw(entry.Path)
			if !exists {
				if field.Required && field.Default == "" {
					return fmt.Errorf("%s: обязательное поле", entry.Path)
				}
				continue
			}
			if field.Required && (value == nil || value == "") {
				return fmt.Errorf("%s: обязательное поле", entry.Path)
			}
			var raw string
			if field.Kind == KindList {
				if _, ok := value.([]any); !ok {
					return fmt.Errorf("%s: нужен массив строк", entry.Path)
				}
			}
			if field.Kind == KindPairs || field.Kind == KindMap {
				if _, ok := value.(map[string]any); !ok {
					return fmt.Errorf("%s: нужен объект со строковыми значениями", entry.Path)
				}
			}
			switch field.Kind {
			case KindList, KindPairs, KindMap, KindJSON, KindSecretJSON:
				data, err := json.Marshal(value)
				if err != nil {
					return err
				}
				raw = string(data)
			case KindBool:
				if _, ok := value.(bool); !ok {
					return fmt.Errorf("%s: нужен bool", entry.Path)
				}
				raw = fmt.Sprint(value)
			case KindInt:
				switch value.(type) {
				case float64, int:
				default:
					return fmt.Errorf("%s: нужно число", entry.Path)
				}
				raw = fmt.Sprint(value)
			default:
				text, ok := value.(string)
				if !ok {
					return fmt.Errorf("%s: нужна строка", entry.Path)
				}
				raw = text
			}
			if _, err := parse(field, raw); err != nil {
				return fmt.Errorf("%s: значение не соответствует схеме", entry.Path)
			}
		}
		return nil
	}
	var descend func(string) error
	descend = func(path string) error {
		collections, err := d.Collections(path)
		if err != nil {
			return nil
		}
		for _, collection := range collections {
			for _, entry := range collection.Entries {
				if _, exists := d.raw(entry.Path); !exists {
					continue
				}
				fields, err := d.EntryFields(entry.Path)
				if err != nil {
					return err
				}
				if err := check(fields); err != nil {
					return err
				}
				if err := descend(entry.Path); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, section := range d.Sections() {
		fields, err := d.Section(section.Key)
		if err != nil {
			return err
		}
		if err := check(fields); err != nil {
			return err
		}
		if err := descend(section.Key); err != nil {
			return err
		}
	}
	return nil
}
