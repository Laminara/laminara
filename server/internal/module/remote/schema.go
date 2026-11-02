package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	sdkmodule "github.com/laminara/laminara/sdk/go/module"
	"github.com/laminara/laminara/server/internal/settings"
	"path/filepath"
)

func (l *Loader) registerSchemas(path string, manifest sdkmodule.Manifest) error {
	if len(manifest.SettingsSchemaJSON) == 0 {
		return nil
	}
	if len(manifest.SettingsSchemaJSON) > 64<<10 {
		return errors.New("схема модуля превышает 64 КиБ")
	}
	var schema struct {
		Module    []settings.SchemaField            `json:"module"`
		Providers map[string][]settings.SchemaField `json:"providers"`
	}
	decoder := json.NewDecoder(bytes.NewReader(manifest.SettingsSchemaJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&schema); err != nil {
		return err
	}
	var installed []func()
	register := func(kind, name string, fields []settings.SchemaField) error {
		if len(fields) == 0 {
			return nil
		}
		remove, err := settings.RegisterSchema(kind, name, fields)
		if err != nil {
			return err
		}
		installed = append(installed, remove)
		return nil
	}
	if err := register("module", filepath.Base(path), schema.Module); err != nil {
		return err
	}
	for _, provider := range manifest.Providers {
		kind := map[sdkmodule.ProviderKind]string{sdkmodule.ProviderAuth: "auth", sdkmodule.ProviderSkin: "skin", sdkmodule.ProviderAccess: "access", sdkmodule.ProviderNews: "news"}[provider.Kind]
		if err := register(kind, provider.Name, schema.Providers[kind+"/"+provider.Name]); err != nil {
			for _, remove := range installed {
				remove()
			}
			return err
		}
	}
	l.schemaCleanup = append(l.schemaCleanup, installed...)
	return nil
}
