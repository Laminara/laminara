package config

import (
	"encoding/json"
	"github.com/laminara/laminara/server/internal/sqlschema"
	"net/url"
	"strings"
	"time"
)

func validateBackends(cfg *Config) error {
	if cfg.Auth != nil {
		var fields struct {
			Path, Driver, DSN, URL, Timeout string
			Headers                         map[string]string
		}
		if len(cfg.Auth.Config) > 0 {
			if err := json.Unmarshal(cfg.Auth.Config, &fields); err != nil {
				return badConfig("auth.config", "неверные типы полей источника аккаунтов")
			}
		}
		switch cfg.Auth.Provider {
		case "jsonfile":
			if strings.TrimSpace(fields.Path) == "" {
				return badConfig("auth.config.path", "укажите файл аккаунтов")
			}
		case "sql":
			if _, err := sqlschema.Canonical(fields.Driver); err != nil {
				return badConfig("auth.config.driver", "укажите поддерживаемую СУБД")
			}
			if fields.DSN == "" {
				return badConfig("auth.config.dsn", "укажите строку подключения")
			}
		case "http":
			parsed, err := url.Parse(fields.URL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return badConfig("auth.config.url", "нужен HTTP или HTTPS адрес")
			}
			if fields.Timeout != "" {
				duration, err := time.ParseDuration(fields.Timeout)
				if err != nil || duration <= 0 {
					return badConfig("auth.config.timeout", "нужна положительная длительность")
				}
			}
		}
	}
	if cfg.Storage != nil {
		var fields struct {
			Root, Endpoint, Bucket, AccessKeyID, SecretAccessKey string
			Secure                                               *bool
			PathStyle                                            bool
		}
		if len(cfg.Storage.Config) > 0 {
			if err := json.Unmarshal(cfg.Storage.Config, &fields); err != nil {
				return badConfig("storage.config", "неверные типы полей хранилища")
			}
		}
		switch cfg.Storage.Backend {
		case "fs":
			if fields.Root == "" {
				return badConfig("storage.config.root", "укажите каталог объектов")
			}
		case "s3":
			if fields.Endpoint == "" || fields.Bucket == "" {
				return badConfig("storage.config", "S3 требует endpoint и bucket")
			}
			if (fields.AccessKeyID == "") != (fields.SecretAccessKey == "") {
				return badConfig("storage.config", "accessKeyId и secretAccessKey задаются вместе")
			}
		}
	}
	return nil
}
