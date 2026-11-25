package sqlstore

import "testing"

func TestDialectUpsertSQL(t *testing.T) {
	cols := []string{"id", "name", "heartbeat_at"}
	values := []string{"?", "?", "NOW()"}
	conflict := []string{"id"}
	updates := []string{"name", "heartbeat_at"}

	mysql := mysqlDialect{}.Upsert("demo", cols, values, conflict, updates)
	wantMySQL := "INSERT INTO `demo` (`id`, `name`, `heartbeat_at`) VALUES (?, ?, NOW()) " +
		"ON DUPLICATE KEY UPDATE `name`=VALUES(`name`), `heartbeat_at`=VALUES(`heartbeat_at`)"
	if mysql != wantMySQL {
		t.Fatalf("mysql upsert:\n got: %s\nwant: %s", mysql, wantMySQL)
	}

	pg := postgresDialect{}.Upsert("demo", cols, values, conflict, updates)
	wantPG := `INSERT INTO "demo" ("id", "name", "heartbeat_at") VALUES ($1, $2, NOW()) ` +
		`ON CONFLICT ("id") DO UPDATE SET "name"=EXCLUDED."name", "heartbeat_at"=EXCLUDED."heartbeat_at"`
	if pg != wantPG {
		t.Fatalf("pg upsert:\n got: %s\nwant: %s", pg, wantPG)
	}

	t.Run("composite conflict target", func(t *testing.T) {
		got := postgresDialect{}.Upsert(
			"demo",
			[]string{"tenant_id", "key_name", "payload"},
			[]string{"?", "?", "?"},
			[]string{"tenant_id", "key_name"},
			[]string{"payload"},
		)
		want := `INSERT INTO "demo" ("tenant_id", "key_name", "payload") VALUES ($1, $2, $3) ` +
			`ON CONFLICT ("tenant_id", "key_name") DO UPDATE SET "payload"=EXCLUDED."payload"`
		if got != want {
			t.Fatalf("pg composite conflict:\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("literal question marks", func(t *testing.T) {
		got := postgresDialect{}.Upsert(
			"demo",
			[]string{"id", "name", "note"},
			[]string{"'?'", "?", `"?"`},
			[]string{"id"},
			[]string{"name"},
		)
		// convertPlaceholdersPG densely numbers actual placeholders; the quoted '?' is not one.
		want := `INSERT INTO "demo" ("id", "name", "note") VALUES ('?', $1, "?") ` +
			`ON CONFLICT ("id") DO UPDATE SET "name"=EXCLUDED."name"`
		if got != want {
			t.Fatalf("pg literal question marks:\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("escaped identifiers", func(t *testing.T) {
		mysql := mysqlDialect{}.Upsert(
			"a`b",
			[]string{"a`b"},
			[]string{"?"},
			[]string{"id"},
			[]string{"a`b"},
		)
		wantMySQL := "INSERT INTO `a``b` (`a``b`) VALUES (?) " +
			"ON DUPLICATE KEY UPDATE `a``b`=VALUES(`a``b`)"
		if mysql != wantMySQL {
			t.Fatalf("mysql escaped identifiers:\n got: %s\nwant: %s", mysql, wantMySQL)
		}

		pg := postgresDialect{}.Upsert(
			`a"b`,
			[]string{`a"b`},
			[]string{"?"},
			[]string{"id"},
			[]string{`a"b`},
		)
		wantPG := `INSERT INTO "a""b" ("a""b") VALUES ($1) ` +
			`ON CONFLICT ("id") DO UPDATE SET "a""b"=EXCLUDED."a""b"`
		if pg != wantPG {
			t.Fatalf("pg escaped identifiers:\n got: %s\nwant: %s", pg, wantPG)
		}
	})
}
