package postgres

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

//go:embed baseline/*.sql
var schemaFiles embed.FS

type schemaMigration struct {
	version       int
	checksum, sql string
}

func schemaMigrations() ([]schemaMigration, error) {
	entries, err := schemaFiles.ReadDir("baseline")
	if err != nil {
		return nil, err
	}
	migrations := make([]schemaMigration, 0, len(entries))
	seen := map[int]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.Atoi(prefix)
		if err != nil || version < 1 || seen[version] {
			return nil, fmt.Errorf("invalid or duplicate migration version %q", entry.Name())
		}
		seen[version] = true
		sql, err := schemaFiles.ReadFile("baseline/" + entry.Name())
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, schemaMigration{version: version, checksum: fmt.Sprintf("%x", sha256.Sum256(sql)), sql: string(sql)})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	if len(migrations) == 0 || migrations[0].version != 1 {
		return nil, fmt.Errorf("missing schema baseline")
	}
	return migrations, nil
}

// Migrate installs the fresh baseline, verifies checksums of every applied
// version, and atomically applies pending ordered forward migrations. Unknown
// schemas/history are rejected; historical private-app migrations are not run.
func Migrate(ctx context.Context, dsn string) error {
	migrations, err := schemaMigrations()
	if err != nil {
		return err
	}
	return migrateVersions(ctx, dsn, migrations)
}

func migrateVersions(ctx context.Context, dsn string, migrations []schemaMigration) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(4670231001)`); err != nil {
		return err
	}
	var tracked bool
	if err = tx.QueryRow(ctx, `SELECT to_regclass('public.fact0_schema_migrations') IS NOT NULL`).Scan(&tracked); err != nil {
		return err
	}
	applied := map[int]string{}
	if tracked {
		rows, err := tx.Query(ctx, `SELECT version,checksum FROM fact0_schema_migrations ORDER BY version`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var version int
			var checksum string
			if err = rows.Scan(&version, &checksum); err != nil {
				rows.Close()
				return err
			}
			applied[version] = checksum
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(applied) == 0 {
			return fmt.Errorf("empty migration history on existing database; refusing unknown schema")
		}
	} else {
		var objects int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema' AND c.relkind IN ('r','p','v','m','S','f')`).Scan(&objects); err != nil {
			return err
		}
		if objects != 0 {
			return fmt.Errorf("database has an unknown nonempty schema; OSS v1 supports fresh installations only")
		}
		if _, err = tx.Exec(ctx, `CREATE TABLE fact0_schema_migrations(version integer PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
			return err
		}
	}
	known := map[int]schemaMigration{}
	for _, m := range migrations {
		known[m.version] = m
	}
	for version, checksum := range applied {
		m, ok := known[version]
		if !ok || m.checksum != checksum {
			return fmt.Errorf("unsupported or modified schema version %d; refusing to change existing database", version)
		}
	}
	missing := false
	for _, m := range migrations {
		if _, ok := applied[m.version]; ok {
			if missing {
				return fmt.Errorf("migration history has a gap before version %d", m.version)
			}
			continue
		}
		missing = true
	}
	for _, m := range migrations {
		if _, ok := applied[m.version]; ok {
			continue
		}
		if _, err = tx.Exec(ctx, m.sql); err != nil {
			return fmt.Errorf("applying schema version %d: %w", m.version, err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO fact0_schema_migrations(version,checksum) VALUES($1,$2)`, m.version, m.checksum); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
