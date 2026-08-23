package sqlschema

import (
	"fmt"
	"regexp"
	"strings"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func Dialect(name string) (driver string, quote func(string) string, placeholder string, err error) {
	switch name {
	case "postgres", "postgresql", "pgx":
		return "pgx", func(s string) string { return `"` + s + `"` }, "$1", nil
	case "mysql", "mariadb":
		return "mysql", func(s string) string { return "`" + s + "`" }, "?", nil
	default:
		return "", nil, "", fmt.Errorf("unsupported sql driver %q", name)
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
