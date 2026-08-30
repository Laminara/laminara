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
	_ "modernc.org/sqlite"

	"github.com/laminara/laminara/server/internal/sqlschema"
)

const textureColumns = 3

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
	db       *sql.DB
	query    string
	byUUID   bool
	slim     bool
	capePos  int
	modelPos int
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
		return &sqlProvider{db: db, query: cfg.Query, byUUID: byUUID, slim: cfg.Slim, capePos: 1, modelPos: 2}, nil
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
	capePos, modelPos := -1, -1
	for _, optional := range []struct {
		field    string
		position *int
	}{{cfg.Fields.Cape, &capePos}, {cfg.Fields.Model, &modelPos}} {
		if optional.field == "" {
			continue
		}
		_, column, err := sqlschema.Field(cfg.Table, optional.field)
		if err != nil {
			return nil, err
		}
		*optional.position = len(columns)
		columns = append(columns, quote(column))
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s LIMIT 1", strings.Join(columns, ", "), quote(table), quote(lookupCol), placeholder)
	return &sqlProvider{db: db, query: query, byUUID: byUUID, slim: cfg.Slim, capePos: capePos, modelPos: modelPos}, nil
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
	if len(columns) == 0 || len(columns) > textureColumns {
		return Textures{}, fmt.Errorf("sql skin query must select 1 to %d columns, got %d", textureColumns, len(columns))
	}
	values := make([]sql.NullString, len(columns))
	targets := make([]any, len(values))
	for i := range values {
		targets[i] = &values[i]
	}
	if err := rows.Scan(targets...); err != nil {
		return Textures{}, err
	}
	at := func(position int) sql.NullString {
		if position < 0 || position >= len(values) {
			return sql.NullString{}
		}
		return values[position]
	}
	textures := Textures{SkinURL: at(0).String, CapeURL: at(p.capePos).String, Slim: p.slim}
	if model := at(p.modelPos); model.Valid && model.String != "" {
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
