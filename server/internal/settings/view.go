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
	section, ok := d.sectionOf(key)
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
		if _, known := field.Variants[chosen]; !known {
			entries = append(entries, d.entryOf(section.Key, field, field.Key))
			continue
		}
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
	field = d.protectedField(base, field)
	raw, present := d.raw(path)
	shownDefault := displayDefault(field)
	if !present && d.namespace == "" {
		if derived := d.derived(path); derived != "" {
			shownDefault = derived
		}
	}
	entry := Entry{
		Path:    path,
		Label:   field.Label,
		Hint:    field.Hint,
		Kind:    field.Kind,
		Default: shownDefault,
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
	safe := redactTree(path, raw, sensitivePaths(path, field))
	if field.Kind == KindSecretJSON {
		return renderTree("", safe, nil)
	}
	if field.Kind == KindPairs || field.Kind == KindList || field.Variants != nil {
		return render(KindJSON, safe)
	}
	return render(field.Kind, safe)
}

func (d *Doc) protectedField(base string, field Field) Field {
	if field.Variants == nil || field.VariantOf == "" {
		return field
	}
	fields, _ := d.fieldsAt(base)
	chosen := d.stringAt(base+"."+field.VariantOf, fieldDefault(fields, field.VariantOf))
	if nested := field.Variants[chosen]; len(nested) > 0 && nested[0].Extension {
		field.Kind, field.Variants = KindSecretJSON, nil
		field.Hint = "Поля адаптера доступны отдельно. Полный JSON скрыт, чтобы не раскрывать неизвестные поля."
		return field
	}
	if _, known := field.Variants[chosen]; !known {
		field.Kind = KindSecretJSON
		field.Variants = nil
		field.Hint = "У адаптера нет схемы настроек. JSON скрыт; введите весь блок для замены."
	}
	return field
}

func display(kind Kind, value string) string {
	if kind == KindList && strings.HasPrefix(value, "[") {
		var entries []string
		if json.Unmarshal([]byte(value), &entries) == nil {
			return strings.Join(entries, ", ")
		}
	}
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
		return "", Field{}, "", fmt.Errorf("укажите полный путь настройки")
	}
	section, ok := d.sectionOf(sectionKey)
	if !ok {
		return "", Field{}, "", fmt.Errorf("раздела «%s» нет", sectionKey)
	}
	return d.resolveIn(sectionKey, section.Fields, section.Collections, rest)
}

func (d *Doc) resolveIn(base string, fields []Field, collections []Collection, rest string) (string, Field, string, error) {
	for _, collection := range collections {
		tail, found := strings.CutPrefix(rest, collection.Key+".")
		if !found {
			continue
		}
		id, fieldKey, hasField := strings.Cut(tail, ".")
		if id == "" {
			return "", Field{}, "", fmt.Errorf("не указана запись списка")
		}
		entryPath := base + "." + collection.Key + "." + id
		collection = collection.forEntry(id)
		if hasField {
			return d.resolveIn(entryPath, collection.Fields, collection.children(), fieldKey)
		}
		if base == "modules" && collection.Key == "config" {
			return entryPath, Field{Label: collection.Title, Kind: KindSecretJSON}, "", nil
		}
		if len(collection.Fields) == 1 && collection.Fields[0].Key == "" {
			return entryPath, collection.Fields[0], "", nil
		}
		protected := make([]Field, 0, len(collection.Fields))
		for _, field := range collection.Fields {
			protected = append(protected, d.protectedField(entryPath, field))
		}
		return entryPath, Field{Label: collection.Title, Kind: KindJSON, Variants: map[string][]Field{"": protected}}, "", nil
	}
	field, ok := d.fieldAt(base, fields, rest)
	if !ok {
		return "", Field{}, "", fmt.Errorf("нет настройки %s.%s", base, rest)
	}
	return base, field, rest, nil
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
	if kind == KindPairs {
		return renderPairs(raw)
	}
	return renderTree("", raw, nil)
}

func renderPairs(raw any) string {
	pairs, ok := raw.(map[string]any)
	if !ok {
		return renderTree("", raw, nil)
	}
	keys := make([]string, 0, len(pairs))
	for key := range pairs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := renderTree("", pairs[key], nil)
		if value != "" && carriesSecret(key) {
			value = SecretMask
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, ", ")
}

func carriesSecret(name string) bool {
	return HeaderSecret(name)
}

func HeaderSecret(name string) bool {
	switch strings.ToLower(name) {
	case "accept", "accept-encoding", "accept-language", "content-type", "user-agent", "cache-control":
		return false
	default:
		return true
	}
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
	return d.set(path, value, false)
}

func (d *Doc) Edit(path, value string) error {
	return d.set(path, value, true)
}

func (d *Doc) set(path, value string, preserveSecrets bool) error {
	base, field, relative, err := d.resolve(path)
	if err != nil {
		return err
	}
	if field.Kind != KindSecret {
		value = strings.TrimSpace(value)
	}
	if value == "" {
		return d.drop(path)
	}
	field = d.protectedField(base, field)
	if preserveSecrets && (field.Kind == KindSecret || field.Kind == KindSecretJSON) && value == SecretMask {
		if _, exists := d.raw(path); !exists {
			return fmt.Errorf("у %s нет сохранённого значения; введите новое", path)
		}
		return nil
	}
	parsed, err := parse(field, value)
	if err != nil {
		return err
	}
	if preserveSecrets {
		previous, _ := d.raw(path)
		if err := validateVariantEdit(field, parsed, previous); err != nil {
			return err
		}
		parsed, err = restoreSecrets(path, parsed, previous, sensitivePaths(path, field))
		if err != nil {
			return err
		}
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
	if section, ok := d.sectionOf(base); ok {
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
	if field.Variants != nil {
		field.Kind = KindJSON
	}
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
		if field.AllowZero {
			parsed, err := duration.Parse(value)
			if err != nil {
				return nil, err
			}
			if parsed < 0 {
				return nil, fmt.Errorf("время не должно быть отрицательным")
			}
			return parsed.Compact(), nil
		}
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
		if strings.HasPrefix(value, "[") {
			var entries []string
			if err := json.Unmarshal([]byte(value), &entries); err != nil {
				return nil, fmt.Errorf("нужен JSON-массив строк")
			}
			result := make([]any, len(entries))
			for i, entry := range entries {
				result[i] = entry
			}
			return result, nil
		}
		parts := []any{}
		for _, part := range strings.Split(value, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				parts = append(parts, trimmed)
			}
		}
		return parts, nil
	case KindJSON, KindSecretJSON:
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return nil, fmt.Errorf("это должен быть корректный JSON")
		}
		return parsed, nil
	case KindPairs:
		if strings.HasPrefix(value, "{") {
			var pairs map[string]string
			if err := json.Unmarshal([]byte(value), &pairs); err != nil {
				return nil, fmt.Errorf("нужен JSON-объект со строковыми значениями")
			}
			result := make(map[string]any, len(pairs))
			for key, item := range pairs {
				if strings.TrimSpace(key) == "" {
					return nil, fmt.Errorf("имя заголовка не должно быть пустым")
				}
				result[key] = item
			}
			return result, nil
		}
		pairs := map[string]any{}
		for _, part := range strings.Split(value, ",") {
			key, item, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok {
				return nil, fmt.Errorf("пишите пары через знак равенства: Ключ=значение")
			}
			if strings.TrimSpace(key) == "" {
				return nil, fmt.Errorf("имя заголовка не должно быть пустым")
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
