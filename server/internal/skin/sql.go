package skin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/laminara/laminara/server/internal/sqlschema"
)

func init() {
	Register("sql", newSQL)
}

type sqlConfig struct {
	Driver string `json:"driver"`
	DSN    string `json:"dsn"`
	Table  string `json:"table"`
	Lookup string `json:"lookup"`
	Fields struct {
		Username string `json:"username"`
		UUID     string `json:"uuid"`
		Skin     string `json:"skin"`
		Cape     string `json:"cape"`
		Model    string `json:"model"`
	} `json:"fields"`
	Query string `json:"query"`
	Slim  bool   `json:"slim"`
}

type sqlProvider struct {
	db     *sql.DB
	query  string
	byUUID bool
	slim   bool
}

func newSQL(raw json.RawMessage) (Provider, error) {
	var cfg sqlConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	driver, quote, placeholder, err := sqlschema.Dialect(cfg.Driver)
	if err != nil {
		return nil, err
	}
	byUUID, err := lookupByUUID(cfg.Lookup)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(driver, cfg.DSN)
	if err != nil {
		return nil, err
	}
	if cfg.Query != "" {
		return &sqlProvider{db: db, query: cfg.Query, byUUID: byUUID, slim: cfg.Slim}, nil
	}

	lookupField := cfg.Fields.Username
	if byUUID {
		lookupField = cfg.Fields.UUID
	}
	table, lookupCol, err := sqlschema.Field(cfg.Table, lookupField)
	if err != nil {
		return nil, err
	}
	if cfg.Fields.Skin == "" {
		return nil, errors.New("sql skin provider requires fields.skin")
	}
	_, skinCol, err := sqlschema.Field(cfg.Table, cfg.Fields.Skin)
	if err != nil {
		return nil, err
	}
	columns := []string{quote(skinCol)}
	for _, optional := range []string{cfg.Fields.Cape, cfg.Fields.Model} {
		if optional == "" {
			break
		}
		_, column, err := sqlschema.Field(cfg.Table, optional)
		if err != nil {
			return nil, err
		}
		columns = append(columns, quote(column))
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s LIMIT 1", strings.Join(columns, ", "), quote(table), quote(lookupCol), placeholder)
	return &sqlProvider{db: db, query: query, byUUID: byUUID, slim: cfg.Slim}, nil
}

func (p *sqlProvider) Textures(ctx context.Context, username, uuid string) (Textures, error) {
	key := username
	if p.byUUID {
		key = uuid
	}
	rows, err := p.db.QueryContext(ctx, p.query, key)
	if err != nil {
		return Textures{}, err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Textures{}, err
		}
		return Textures{Slim: p.slim}, nil
	}
	columns, err := rows.Columns()
	if err != nil {
		return Textures{}, err
	}
	var skinURL, capeURL, model sql.NullString
	targets := []any{&skinURL, &capeURL, &model}
	if len(columns) == 0 || len(columns) > len(targets) {
		return Textures{}, fmt.Errorf("sql skin query must select 1 to %d columns, got %d", len(targets), len(columns))
	}
	if err := rows.Scan(targets[:len(columns)]...); err != nil {
		return Textures{}, err
	}
	textures := Textures{SkinURL: skinURL.String, CapeURL: capeURL.String, Slim: p.slim}
	if model.Valid && model.String != "" {
		textures.Slim = strings.EqualFold(model.String, "slim")
	}
	return textures, nil
}

func lookupByUUID(name string) (bool, error) {
	switch name {
	case "", "username", "name":
		return false, nil
	case "uuid":
		return true, nil
	default:
		return false, fmt.Errorf("unsupported skin lookup %q", name)
	}
}
