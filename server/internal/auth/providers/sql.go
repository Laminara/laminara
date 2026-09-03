package providers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/laminara/laminara/server/internal/sqlschema"

	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/auth/hash"
	"github.com/laminara/laminara/server/internal/auth/totp"
)

func init() {
	auth.RegisterProvider("sql", newSQL)
}

type sqlConfig struct {
	Driver string `json:"driver"`
	DSN    string `json:"dsn"`
	Table  string `json:"table"`
	Hash   string `json:"hash"`
	Fields struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		UUID      string `json:"uuid"`
		TwoFactor string `json:"twoFactorSecret"`
	} `json:"fields"`
	Query string `json:"query"`
}

type sqlProvider struct {
	db        *sql.DB
	query     string
	custom    bool
	hasUUID   bool
	hasSecret bool
	verifier  hash.Verifier
	scheme    string
	second    *totp.Verifier
}

func newSQL(raw json.RawMessage) (auth.Provider, error) {
	var cfg sqlConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	driver, quote, placeholder, err := sqlschema.Dialect(cfg.Driver)
	if err != nil {
		return nil, err
	}
	scheme := orDefault(cfg.Hash, "argon2id")
	verifier, err := verifierFor(scheme)
	if err != nil {
		return nil, err
	}
	if cfg.Query != "" {
		if cfg.Fields.TwoFactor != "" {
			return nil, errors.New("sql auth: two-factor column works with table mode only, not with a custom query")
		}
		db, err := sql.Open(driver, cfg.DSN)
		if err != nil {
			return nil, err
		}
		return &sqlProvider{db: db, query: cfg.Query, custom: true, verifier: verifier, scheme: scheme}, nil
	}
	table, usernameCol, err := sqlschema.Field(cfg.Table, cfg.Fields.Username)
	if err != nil {
		return nil, err
	}
	_, passwordCol, err := sqlschema.Field(cfg.Table, cfg.Fields.Password)
	if err != nil {
		return nil, err
	}
	columns := quote(passwordCol)
	hasUUID := cfg.Fields.UUID != ""
	if hasUUID {
		_, uuidCol, err := sqlschema.Field(cfg.Table, cfg.Fields.UUID)
		if err != nil {
			return nil, err
		}
		columns += ", " + quote(uuidCol)
	}
	hasSecret := cfg.Fields.TwoFactor != ""
	if hasSecret {
		_, secretCol, err := sqlschema.Field(cfg.Table, cfg.Fields.TwoFactor)
		if err != nil {
			return nil, err
		}
		columns += ", " + quote(secretCol)
	}
	db, err := sql.Open(driver, cfg.DSN)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s",
		columns, quote(table), quote(usernameCol), placeholder)
	return &sqlProvider{db: db, query: query, hasUUID: hasUUID, hasSecret: hasSecret, verifier: verifier, scheme: scheme, second: totp.NewVerifier()}, nil
}

func (p *sqlProvider) Authenticate(ctx context.Context, creds auth.Credentials) (auth.Identity, error) {
	stored, rawUUID, rawSecret, err := p.lookup(ctx, creds.Username)
	if err != nil {
		return auth.Identity{}, err
	}
	valid, err := verify(p.verifier, p.scheme, creds.Password, stored)
	if err != nil {
		return auth.Identity{}, err
	}
	if !valid {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	if secret := strings.TrimSpace(rawSecret.String); p.hasSecret && secret != "" {
		if creds.TwoFactorCode == "" {
			return auth.Identity{}, auth.ErrTwoFactorRequired
		}
		if !p.second.Verify(creds.Username, secret, creds.TwoFactorCode) {
			return auth.Identity{}, auth.ErrInvalidCredentials
		}
	}
	id := auth.OfflineUUID(creds.Username)
	if rawUUID.Valid {
		if parsed, err := uuid.Parse(rawUUID.String); err == nil {
			id = parsed
		}
	}
	return auth.Identity{Subject: creds.Username, Username: creds.Username, UUID: id}, nil
}

func (p *sqlProvider) lookup(ctx context.Context, username string) (string, sql.NullString, sql.NullString, error) {
	var stored string
	var rawUUID sql.NullString
	var rawSecret sql.NullString

	if !p.custom {
		dest := []any{&stored}
		if p.hasUUID {
			dest = append(dest, &rawUUID)
		}
		if p.hasSecret {
			dest = append(dest, &rawSecret)
		}
		if err := p.db.QueryRowContext(ctx, p.query, username).Scan(dest...); err != nil {
			return "", rawUUID, rawSecret, translate(err)
		}
		return stored, rawUUID, rawSecret, nil
	}

	rows, err := p.db.QueryContext(ctx, p.query, username)
	if err != nil {
		return "", rawUUID, rawSecret, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", rawUUID, rawSecret, err
	}
	if len(columns) == 0 || len(columns) > 2 {
		return "", rawUUID, rawSecret, fmt.Errorf("auth query must select the password and optionally the uuid, got %d columns", len(columns))
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", rawUUID, rawSecret, err
		}
		return "", rawUUID, rawSecret, auth.ErrInvalidCredentials
	}
	dest := []any{&stored}
	if len(columns) == 2 {
		dest = append(dest, &rawUUID)
	}
	if err := rows.Scan(dest...); err != nil {
		return "", rawUUID, rawSecret, err
	}
	return stored, rawUUID, rawSecret, rows.Err()
}

func translate(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return auth.ErrInvalidCredentials
	}
	return err
}
