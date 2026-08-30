package sqlschema_test

import (
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/sqlschema"
)

func TestDriversAreCanonicalAndDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, driver := range sqlschema.Drivers() {
		if seen[driver] {
			t.Fatalf("driver %q is listed twice", driver)
		}
		seen[driver] = true
		canonical, err := sqlschema.Canonical(driver)
		if err != nil {
			t.Fatalf("%s: %v", driver, err)
		}
		if canonical != driver {
			t.Fatalf("%s resolves to %s", driver, canonical)
		}
	}
	if !seen[sqlschema.SQLite] {
		t.Fatal("sqlite must be offered: the hwid store opens it by default")
	}
}

func TestDialectAcceptsAliases(t *testing.T) {
	for alias, driver := range map[string]string{
		"postgres":   sqlschema.Postgres,
		"postgresql": sqlschema.Postgres,
		"pgx":        sqlschema.Postgres,
		"mysql":      sqlschema.MySQL,
		"mariadb":    sqlschema.MySQL,
		"sqlite":     sqlschema.SQLite,
	} {
		open, _, placeholder, err := sqlschema.Dialect(alias)
		if err != nil {
			t.Fatalf("%s: %v", alias, err)
		}
		if open == "" || placeholder == "" {
			t.Fatalf("%s has no driver or placeholder", alias)
		}
		canonical, err := sqlschema.Canonical(alias)
		if err != nil || canonical != driver {
			t.Fatalf("%s = %s, %v", alias, canonical, err)
		}
	}
	if _, _, _, err := sqlschema.Dialect("oracle"); err == nil {
		t.Fatal("an unknown driver must be refused")
	}
	if _, err := sqlschema.Canonical("oracle"); err == nil {
		t.Fatal("an unknown driver must be refused")
	}
}

func TestDialectQuotesAndPlaceholders(t *testing.T) {
	_, quote, placeholder, err := sqlschema.Dialect(sqlschema.MySQL)
	if err != nil {
		t.Fatal(err)
	}
	if quoted := quote("hwid_machine"); !strings.HasPrefix(quoted, "`") || !strings.HasSuffix(quoted, "`") {
		t.Fatalf("quoted = %q", quoted)
	}
	if placeholder != "?" {
		t.Fatalf("placeholder = %q", placeholder)
	}

	_, quote, placeholder, err = sqlschema.Dialect(sqlschema.Postgres)
	if err != nil {
		t.Fatal(err)
	}
	if quoted := quote("hwid_machine"); !strings.HasPrefix(quoted, `"`) || !strings.HasSuffix(quoted, `"`) {
		t.Fatalf("quoted = %q", quoted)
	}
	if placeholder != "$1" {
		t.Fatalf("placeholder = %q", placeholder)
	}

	_, quote, placeholder, err = sqlschema.Dialect(sqlschema.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	if quoted := quote("hwid_machine"); quoted != `"hwid_machine"` {
		t.Fatalf("quoted = %q", quoted)
	}
	if placeholder != "?" {
		t.Fatalf("placeholder = %q", placeholder)
	}
}

func TestField(t *testing.T) {
	table, column, err := sqlschema.Field("users", "fields.username")
	if err != nil {
		t.Fatal(err)
	}
	if table != "fields" || column != "username" {
		t.Fatalf("table = %q, column = %q", table, column)
	}
	if _, _, err := sqlschema.Field("users", "drop--table"); err == nil {
		t.Fatal("an identifier that is not a name must be refused")
	}
}
