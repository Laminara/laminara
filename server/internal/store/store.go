package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "modernc.org/sqlite"
)

type Driver string

const (
	DriverPostgres Driver = "postgres"
	DriverMySQL    Driver = "mysql"
	DriverSQLite   Driver = "sqlite"
)

type Config struct {
	Driver Driver
	DSN    string
}

func sqliteDSN(dsn string) (string, error) {
	if dsn == "" {
		return "", fmt.Errorf("sqlite needs a file path")
	}
	path, query, _ := strings.Cut(dsn, "?")
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return "", err
		}
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return "", err
	}
	for key, value := range map[string]string{
		"_pragma":      "",
		"_time_format": "sqlite",
	} {
		if value != "" && values.Get(key) == "" {
			values.Set(key, value)
		}
	}
	pragmas := values["_pragma"]
	has := func(name string) bool {
		for _, pragma := range pragmas {
			if strings.HasPrefix(pragma, name) {
				return true
			}
		}
		return false
	}
	for _, pragma := range []string{"journal_mode(WAL)", "busy_timeout(5000)", "foreign_keys(ON)"} {
		if name, _, _ := strings.Cut(pragma, "("); !has(name) {
			pragmas = append(pragmas, pragma)
		}
	}
	values["_pragma"] = pragmas
	return path + "?" + values.Encode(), nil
}

func Open(cfg Config) (*bun.DB, error) {
	switch cfg.Driver {
	case DriverPostgres:
		sqldb, err := sql.Open("pgx", cfg.DSN)
		if err != nil {
			return nil, err
		}
		return bun.NewDB(sqldb, pgdialect.New()), nil
	case DriverMySQL:
		sqldb, err := sql.Open("mysql", cfg.DSN)
		if err != nil {
			return nil, err
		}
		return bun.NewDB(sqldb, mysqldialect.New()), nil
	case DriverSQLite:
		dsn, err := sqliteDSN(cfg.DSN)
		if err != nil {
			return nil, err
		}
		sqldb, err := sql.Open("sqlite", dsn)
		if err != nil {
			return nil, err
		}
		sqldb.SetMaxOpenConns(1)
		return bun.NewDB(sqldb, sqlitedialect.New()), nil
	default:
		return nil, fmt.Errorf("unsupported database driver %q", cfg.Driver)
	}
}
