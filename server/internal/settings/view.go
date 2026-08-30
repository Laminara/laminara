package settings

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/laminara/laminara/server/internal/duration"
	"github.com/laminara/laminara/server/internal/humanize"
)

const SecretMask = "***"

type Entry struct {
	Path    string
	Label   string
	Hint    string
	Kind    Kind
	Value   string
	Display string
	Default string
	IsSet   bool
	Options []string
}

func (d *Doc) Section(key string) ([]Entry, error) {
	section, ok := sectionOf(key)
	if !ok {
		return nil, fmt.Errorf("раздела «%s» нет", key)
	}
	var entries []Entry
	for _, field := range section.Fields {
		if field.Variants == nil {
			entries = append(entries, d.entryOf(section.Key, field, field.Key))
			continue
		}
		chosen := d.stringAt(section.Key+"."+field.VariantOf, defaultOf(section, field.VariantOf))
		for _, nested := range field.Variants[chosen] {
			entries = append(entries, d.entryOf(section.Key, nested, field.Key+"."+nested.Key))
		}
	}
	return entries, nil
}

func defaultOf(section Section, key string) string {
	for _, field := range section.Fields {
		if field.Key == key {
			return field.Default
		}
	}
	return ""
}

func (d *Doc) entryOf(base string, field Field, relative string) Entry {
	path := base
	if relative != "" {
		path = base + "." + relative
	}
	raw, present := d.raw(path)
	entry := Entry{
		Path:    path,
		Label:   field.Label,
		Hint:    field.Hint,
		Kind:    field.Kind,
		Default: displayDefault(field),
		IsSet:   present,
		Options: field.options(),
	}
	if present {
		entry.Value = renderEntry(field, path, raw)
	} else {
		entry.Value = entry.Default
	}
	entry.Display = display(field.Kind, entry.Value)
	return entry
}

func renderEntry(field Field, path string, raw any) string {
	if len(field.Variants) == 0 {
		return render(field.Kind, raw)
	}
	return renderTree(path, raw, secretPaths(path, field))
}

func secretPaths(base string, field Field) map[string]bool {
	paths := map[string]bool{}
	collectSecrets(base+".", field.Variants, paths)
	return paths
}

func collectSecrets(prefix string, variants map[string][]Field, into map[string]bool) {
	for _, fields := range variants {
		for _, field := range fields {
			key := prefix + field.Key
			if field.Variants != nil {
				collectSecrets(key+".", field.Variants, into)
				continue
			}
			if field.Kind == KindSecret {
				into[key] = true
			}
		}
	}
}

func display(kind Kind, value string) string {
	if kind != KindDuration || value == "" {
		return value
	}
	parsed, err := duration.Parse(value)
	if err != nil {
		return value
	}
	spelled := humanize.Duration(parsed.Duration())
	if spelled == value {
		return value
	}
	return spelled
}

func displayDefault(field Field) string {
	if field.Default == "" {
		return ""
	}
	parsed, err := parse(field, field.Default)
	if err != nil {
		return field.Default
	}
	return render(field.Kind, parsed)
}

func (d *Doc) stringAt(path, fallback string) string {
	raw, ok := d.raw(path)
	if !ok {
		return fallback
	}
	if text, ok := raw.(string); ok && text != "" {
		return text
	}
	return fallback
}

func (d *Doc) Entry(path string) (Entry, error) {
	base, field, relative, err := d.resolve(path)
	if err != nil {
		return Entry{}, err
	}
	return d.entryOf(base, field, relative), nil
}

func (d *Doc) resolve(path string) (string, Field, string, error) {
	sectionKey, rest, ok := strings.Cut(path, ".")
	if !ok {
		return "", Field{}, "", fmt.Errorf("укажите полный путь, например api.addr")
	}
	section, ok := sectionOf(sectionKey)
	if !ok {
		return "", Field{}, "", fmt.Errorf("раздела «%s» нет", sectionKey)
	}
	if head, tail, ok := strings.Cut(rest, "."); ok {
		if collection, found := collectionOf(section, head); found {
			id, fieldKey, hasField := strings.Cut(tail, ".")
			base := sectionKey + "." + head + "." + id
			if !hasField {
				return base, Field{Key: "", Label: collection.Title, Kind: KindJSON}, "", nil
			}
			field, ok := d.fieldAt(base, collection.Fields, fieldKey)
			if !ok {
				return "", Field{}, "", fmt.Errorf("в списке «%s» нет настройки «%s»", collection.Title, fieldKey)
			}
			return base, field, fieldKey, nil
		}
	}
	field, ok := d.fieldAt(sectionKey, section.Fields, rest)
	if !ok {
		return "", Field{}, "", fmt.Errorf("в разделе «%s» нет настройки «%s»", section.Title, rest)
	}
	return sectionKey, field, rest, nil
}

func (d *Doc) fieldAt(base string, fields []Field, key string) (Field, bool) {
	for _, field := range fields {
		if field.Key == key {
			return field, true
		}
		if field.Variants == nil {
			continue
		}
		tail, ok := strings.CutPrefix(key, field.Key+".")
		if !ok {
			continue
		}
		chosen := d.stringAt(base+"."+field.VariantOf, fieldDefault(fields, field.VariantOf))
		if found, ok := d.fieldAt(base, field.Variants[chosen], tail); ok {
			return found, true
		}
	}
	return Field{}, false
}

func render(kind Kind, raw any) string {
	if kind == KindJSON {
		data, err := json.Marshal(raw)
		if err != nil {
			return ""
		}
		return string(data)
	}
	if kind == KindSecret {
		if text, ok := raw.(string); ok && text != "" {
			return SecretMask
		}
	}
	return renderTree("", raw, nil)
}

func renderTree(path string, raw any, secrets map[string]bool) string {
	switch value := raw.(type) {
	case nil:
		return ""
	case bool:
		if value {
			return "да"
		}
		return "нет"
	case float64:
		if value == float64(int64(value)) {
			return strconv.FormatInt(int64(value), 10)
		}
		return strconv.FormatFloat(value, 'f', -1, 64)
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for index, item := range value {
			parts = append(parts, renderBranch(path+"."+strconv.Itoa(index), item, secrets))
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+"="+renderBranch(path+"."+key, value[key], secrets))
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprint(value)
	}
}

func renderBranch(path string, raw any, secrets map[string]bool) string {
	if secrets[path] {
		return SecretMask
	}
	return renderTree(path, raw, secrets)
}

func (d *Doc) Set(path, value string) error {
	base, field, relative, err := d.resolve(path)
	if err != nil {
		return err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return d.drop(path)
	}
	parsed, err := parse(field, value)
	if err != nil {
		return err
	}
	if block, dropped := d.blockOf(base, relative, value); dropped {
		if err := d.drop(base + "." + block); err != nil {
			return err
		}
	}
	if relative == "" {
		return d.put(base, parsed)
	}
	return d.put(path, parsed)
}

func (d *Doc) fieldsAt(base string) ([]Field, string) {
	if section, ok := sectionOf(base); ok {
		return section.Fields, base
	}
	if collection, _, ok := d.collectionAt(base); ok {
		return collection.Fields, base
	}
	return nil, base
}

func (d *Doc) blockOf(base, key, value string) (string, bool) {
	fields, _ := d.fieldsAt(base)
	for _, field := range fields {
		if field.Variants == nil || field.VariantOf != key {
			continue
		}
		if d.stringAt(base+"."+key, fieldDefault(fields, key)) == value {
			return "", false
		}
		return field.Key, true
	}
	return "", false
}

func parse(field Field, value string) (any, error) {
	switch field.Kind {
	case KindBool:
		switch strings.ToLower(value) {
		case "да", "true", "1", "on", "y", "yes", "вкл":
			return true, nil
		case "нет", "false", "0", "off", "n", "no", "выкл":
			return false, nil
		}
		return nil, fmt.Errorf("напишите «да» или «нет», а не «%s»", value)
	case KindInt:
		number, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("нужно целое число, а не «%s»", value)
		}
		return number, nil
	case KindDuration:
		normalized, err := ParseDuration(value)
		if err != nil {
			return nil, err
		}
		return normalized, nil
	case KindChoice:
		options := field.options()
		if len(options) == 0 {
			return value, nil
		}
		for _, option := range options {
			if option == value {
				return value, nil
			}
		}
		return nil, fmt.Errorf("тут выбирают из: %s", strings.Join(options, ", "))
	case KindList:
		parts := []any{}
		for _, part := range strings.Split(value, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
		return parts, nil
	case KindJSON:
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return nil, fmt.Errorf("это должен быть JSON: %w", err)
		}
		return parsed, nil
	case KindPairs:
		pairs := map[string]any{}
		for _, part := range strings.Split(value, ",") {
			key, item, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok {
				return nil, fmt.Errorf("пишите пары через знак равенства: Ключ=значение")
			}
			pairs[strings.TrimSpace(key)] = strings.TrimSpace(item)
		}
		return pairs, nil
	default:
		return value, nil
	}
}

func ParseDuration(value string) (string, error) {
	parsed, err := duration.Parse(value)
	if err != nil {
		return "", err
	}
	if parsed <= 0 {
		return "", fmt.Errorf("время должно быть больше нуля")
	}
	return parsed.Compact(), nil
}
