package providers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
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
	db          *sql.DB
	query       string
	custom      bool
	hasUUID     bool
	hasSecret   bool
	verifier    hash.Verifier
	scheme      string
	second      *totp.Verifier
	driver      string
	table       string
	usernameCol string
	passwordCol string
	uuidCol     string
	quote       func(string) string
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
		tunePool(db)
		return &sqlProvider{db: db, query: cfg.Query, custom: true, verifier: verifier, scheme: scheme, driver: driver, quote: quote}, nil
	}
	table, usernameCol, err := sqlschema.Field(cfg.Table, cfg.Fields.Username)
	if err != nil {
		return nil, err
	}
	_, passwordCol, err := sqlschema.Field(cfg.Table, cfg.Fields.Password)
	if err != nil {
		return nil, err
	}
	columns := quote(usernameCol) + ", " + quote(passwordCol)
	hasUUID := cfg.Fields.UUID != ""
	uuidCol := ""
	if hasUUID {
		_, resolved, err := sqlschema.Field(cfg.Table, cfg.Fields.UUID)
		if err != nil {
			return nil, err
		}
		uuidCol = resolved
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
	tunePool(db)
	query := fmt.Sprintf("SELECT %s FROM %s WHERE LOWER(%s) = LOWER(%s)",
		columns, quote(table), quote(usernameCol), placeholder)
	return &sqlProvider{
		db:          db,
		query:       query,
		hasUUID:     hasUUID,
		hasSecret:   hasSecret,
		verifier:    verifier,
		scheme:      scheme,
		second:      totp.NewVerifier(),
		driver:      driver,
		table:       table,
		usernameCol: usernameCol,
		passwordCol: passwordCol,
		uuidCol:     uuidCol,
		quote:       quote,
	}, nil
}

func tunePool(db *sql.DB) {
	db.SetConnMaxLifetime(3 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)
	db.SetMaxIdleConns(4)
	db.SetMaxOpenConns(16)
}

func (p *sqlProvider) Authenticate(ctx context.Context, creds auth.Credentials) (auth.Identity, error) {
	account, err := p.lookup(ctx, creds.Username)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			spendSameTime(p.verifier, creds.Password)
		}
		return auth.Identity{}, err
	}
	creds.Username = account.name
	stored, rawSecret := account.password, account.secret
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
	return account.identity()
}

type sqlAccount struct {
	name     string
	password string
	uuid     sql.NullString
	secret   sql.NullString
}

func (a sqlAccount) identity() (auth.Identity, error) {
	id, err := accountUUID(a.uuid.String, a.name)
	if err != nil {
		return auth.Identity{}, err
	}
	return auth.Identity{Subject: a.name, Username: a.name, UUID: id}, nil
}

func (p *sqlProvider) lookup(ctx context.Context, username string) (sqlAccount, error) {
	account := sqlAccount{name: username}
	rows, err := p.db.QueryContext(ctx, p.query, username)
	if err != nil {
		return account, err
	}
	defer rows.Close()
	var dest []any
	if p.custom {
		columns, err := rows.Columns()
		if err != nil {
			return account, err
		}
		if len(columns) < 1 || len(columns) > 3 {
			return account, fmt.Errorf("запрос за аккаунтом должен выбирать пароль и, если нужно, UUID и имя игрока, а выбрал %d колонок", len(columns))
		}
		dest = []any{&account.password}
		if len(columns) >= 2 {
			dest = append(dest, &account.uuid)
		}
		if len(columns) == 3 {
			dest = append(dest, &account.name)
		}
	} else {
		dest = []any{&account.name, &account.password}
		if p.hasUUID {
			dest = append(dest, &account.uuid)
		}
		if p.hasSecret {
			dest = append(dest, &account.secret)
		}
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return account, err
		}
		return account, auth.ErrInvalidCredentials
	}
	if err := rows.Scan(dest...); err != nil {
		return account, err
	}
	if rows.Next() {
		return account, fmt.Errorf("по имени найдено несколько аккаунтов")
	}
	return account, rows.Err()
}

func (p *sqlProvider) LookupProfile(ctx context.Context, username string) (auth.Identity, error) {
	account, err := p.lookup(ctx, username)
	if err != nil {
		return auth.Identity{}, err
	}
	return account.identity()
}
