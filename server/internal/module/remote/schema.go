package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	sdkmodule "github.com/laminara/laminara/sdk/go/module"
	"github.com/laminara/laminara/server/internal/settings"
)

func decodeSchema(manifest sdkmodule.Manifest) (sdkmodule.SettingsDescriptor, error) {
	var schema sdkmodule.SettingsDescriptor
	if len(manifest.SettingsSchemaJSON) == 0 {
		return schema, nil
	}
	if len(manifest.SettingsSchemaJSON) > 64<<10 {
		return schema, errors.New("схема модуля превышает 64 КиБ")
	}
	decoder := json.NewDecoder(bytes.NewReader(manifest.SettingsSchemaJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&schema); err != nil {
		return schema, errors.New("описание настроек не соответствует контракту")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return schema, errors.New("после схемы есть лишние данные")
	}
	if schema.Version < 0 || schema.Version > 1 {
		return schema, errors.New("версия схемы не поддерживается")
	}
	known := map[string]bool{}
	for _, provider := range manifest.Providers {
		known[providerKind(provider.Kind)+"/"+provider.Name] = true
	}
	for name := range schema.Providers {
		if !known[name] {
			return schema, fmt.Errorf("схема ссылается на незаявленный адаптер %s", name)
		}
	}
	return schema, nil
}

func providerKind(kind sdkmodule.ProviderKind) string {
	return map[sdkmodule.ProviderKind]string{sdkmodule.ProviderAuth: "auth", sdkmodule.ProviderSkin: "skin", sdkmodule.ProviderAccess: "access", sdkmodule.ProviderNews: "news"}[kind]
}

func (l *Loader) registerSchemas(path string, manifest sdkmodule.Manifest) (sdkmodule.SettingsDescriptor, error) {
	schema, err := decodeSchema(manifest)
	if err != nil {
		return schema, err
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
		return schema, err
	}
	for _, provider := range manifest.Providers {
		kind := providerKind(provider.Kind)
		if err := register(kind, provider.Name, schema.Providers[kind+"/"+provider.Name]); err != nil {
			for _, remove := range installed {
				remove()
			}
			return schema, err
		}
	}
	l.schemaCleanup = append(l.schemaCleanup, installed...)
	return schema, nil
}
