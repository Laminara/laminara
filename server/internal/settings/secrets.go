package settings

import (
	"fmt"
	"strconv"
	"strings"
)

func sensitivePaths(path string, field Field) map[string]Kind {
	paths := make(map[string]Kind)
	var collect func(string, Field)
	collect = func(path string, field Field) {
		switch field.Kind {
		case KindSecret, KindSecretJSON, KindPairs:
			paths[path] = field.Kind
		}
		for _, fields := range field.Variants {
			for _, nested := range fields {
				collect(path+"."+nested.Key, nested)
			}
		}
	}
	collect(path, field)
	return paths
}

func secretAt(path string, paths map[string]Kind) bool {
	if kind := paths[path]; kind == KindSecret || kind == KindSecretJSON {
		return true
	}
	for parent, kind := range paths {
		if kind != KindPairs {
			continue
		}
		if key, ok := strings.CutPrefix(path, parent+"."); ok && carriesSecret(key) {
			return true
		}
	}
	return false
}

func redactTree(path string, value any, paths map[string]Kind) any {
	if secretAt(path, paths) && value != nil && value != "" {
		return SecretMask
	}
	switch value := value.(type) {
	case map[string]any:
		copy := make(map[string]any, len(value))
		for key, item := range value {
			copy[key] = redactTree(path+"."+key, item, paths)
		}
		return copy
	case []any:
		copy := make([]any, len(value))
		for index, item := range value {
			copy[index] = redactTree(path+"."+strconv.Itoa(index), item, paths)
		}
		return copy
	default:
		return value
	}
}

func restoreSecrets(path string, value, previous any, paths map[string]Kind) (any, error) {
	if text, ok := value.(string); ok && text == SecretMask && secretAt(path, paths) {
		if previous == nil {
			return nil, fmt.Errorf("у %s нет сохранённого секрета; введите новое значение", path)
		}
		return previous, nil
	}
	if values, ok := value.(map[string]any); ok {
		old, _ := previous.(map[string]any)
		for key, item := range values {
			restored, err := restoreSecrets(path+"."+key, item, old[key], paths)
			if err != nil {
				return nil, err
			}
			values[key] = restored
		}
	}
	return value, nil
}

func validateVariantEdit(field Field, value, previous any) error {
	current, _ := value.(map[string]any)
	old, _ := previous.(map[string]any)
	for _, fields := range field.Variants {
		for _, nested := range fields {
			currentKind, _ := current[nested.VariantOf].(string)
			oldKind, _ := old[nested.VariantOf].(string)
			if nested.VariantOf != "" && currentKind != oldKind && containsMask(current[nested.Key]) {
				return fmt.Errorf("при смене адаптера введите его секреты заново")
			}
			if err := validateVariantEdit(nested, current[nested.Key], old[nested.Key]); err != nil {
				return err
			}
		}
	}
	return nil
}

func containsMask(value any) bool {
	switch value := value.(type) {
	case string:
		return value == SecretMask
	case map[string]any:
		for _, nested := range value {
			if containsMask(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range value {
			if containsMask(nested) {
				return true
			}
		}
	}
	return false
}
