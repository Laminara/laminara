package sqlschema

import (
	"fmt"
	"regexp"
	"strings"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const (
	Postgres = "postgres"
	MySQL    = "mysql"
	SQLite   = "sqlite"
)

func Drivers() []string { return []string{Postgres, MySQL, SQLite} }

func Canonical(name string) (string, error) {
	switch name {
	case "postgres", "postgresql", "pgx":
		return Postgres, nil
	case "mysql", "mariadb":
		return MySQL, nil
	case SQLite:
		return SQLite, nil
	default:
		return "", fmt.Errorf("unsupported sql driver %q", name)
	}
}

func Dialect(name string) (driver string, quote func(string) string, placeholder string, err error) {
	canonical, err := Canonical(name)
	if err != nil {
		return "", nil, "", err
	}
	switch canonical {
	case Postgres:
		return "pgx", func(s string) string { return `"` + s + `"` }, "$1", nil
	case MySQL:
		return MySQL, func(s string) string { return "`" + s + "`" }, "?", nil
	default:
		return SQLite, func(s string) string { return `"` + s + `"` }, "?", nil
	}
}

func Field(defaultTable, value string) (table, column string, err error) {
	table, column = defaultTable, value
	if i := strings.LastIndex(value, "."); i >= 0 {
		table, column = value[:i], value[i+1:]
	}
	if !identifierPattern.MatchString(table) || !identifierPattern.MatchString(column) {
		return "", "", fmt.Errorf("invalid table/column identifier in %q", value)
	}
	return table, column, nil
}
