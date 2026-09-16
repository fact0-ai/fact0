package postgres

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func migrationDatabase(t *testing.T) (string, *pgx.Conn) {
	t.Helper()
	dsn := os.Getenv("FACT0_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("FACT0_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("fact0_migration_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Close(ctx)
		admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		admin.Close(ctx)
	})
	return u.String(), conn
}

func TestMigrateFreshRerunAndForward(t *testing.T) {
	dsn, conn := migrationDatabase(t)
	ctx := context.Background()
	if err := Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, dsn); err != nil {
		t.Fatal("rerun", err)
	}
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM fact0_schema_migrations`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("history: %d %v", count, err)
	}
	var authTable bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('public.account') IS NOT NULL AND to_regclass('public.jwks') IS NOT NULL`).Scan(&authTable); err != nil || !authTable {
		t.Fatal("BetterAuth schema absent", err)
	}
	if _, err := conn.Exec(ctx, `SELECT alg, crv FROM jwks LIMIT 0`); err != nil {
		t.Fatal("BetterAuth JWT metadata columns absent", err)
	}
	migrations, err := schemaMigrations()
	if err != nil {
		t.Fatal(err)
	}
	sql := `CREATE TABLE core_forward_test (id integer PRIMARY KEY); INSERT INTO core_forward_test VALUES(1);`
	migrations = append(migrations, schemaMigration{version: 3, checksum: fmt.Sprintf("%x", sha256.Sum256([]byte(sql))), sql: sql})
	if err = migrateVersions(ctx, dsn, migrations); err != nil {
		t.Fatal("forward", err)
	}
	if err = migrateVersions(ctx, dsn, migrations); err != nil {
		t.Fatal("forward rerun", err)
	}
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM core_forward_test`).Scan(&count); err != nil || count != 1 {
		t.Fatal("forward repeated", count, err)
	}
	if err = Migrate(ctx, dsn); err == nil {
		t.Fatal("older executable accepted unknown schema version")
	}
	if _, err = conn.Exec(ctx, `UPDATE fact0_schema_migrations SET checksum='modified' WHERE version=1`); err != nil {
		t.Fatal(err)
	}
	if err = migrateVersions(ctx, dsn, migrations); err == nil {
		t.Fatal("modified checksum accepted")
	}
}

func TestMigrateRejectsUnknownNonemptyDatabase(t *testing.T) {
	dsn, conn := migrationDatabase(t)
	ctx := context.Background()
	if _, err := conn.Exec(ctx, `CREATE TABLE unrelated (id integer); INSERT INTO unrelated VALUES(42)`); err != nil {
		t.Fatal(err)
	}
	err := Migrate(ctx, dsn)
	if err == nil || !strings.Contains(err.Error(), "unknown nonempty") {
		t.Fatalf("unknown schema result: %v", err)
	}
	var value int
	if err = conn.QueryRow(ctx, `SELECT id FROM unrelated`).Scan(&value); err != nil || value != 42 {
		t.Fatal("unknown data altered", err)
	}
	var ledger bool
	if err = conn.QueryRow(ctx, `SELECT to_regclass('public.fact0_schema_migrations') IS NOT NULL`).Scan(&ledger); err != nil || ledger {
		t.Fatal("unknown schema was marked migrated", err)
	}
}
