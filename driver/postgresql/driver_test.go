package postgresql

import (
	"context"
	"testing"
	"time"

	"github.com/telemetryos/graviton/config"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"
)

const (
	testDatabaseURL  = "postgres://postgres@localhost:5432/graviton_test?sslmode=disable"
	testDatabaseName = "graviton_test"
)

func setupTestDriver(t *testing.T) (*Driver, context.Context) {
	t.Helper()

	conf := &config.DatabaseConfig{
		ConnectionUrl: testDatabaseURL,
		DatabaseName:  testDatabaseName,
	}

	drv := New(conf)
	ctx := context.Background()

	if err := drv.Connect(ctx); err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}

	t.Cleanup(func() {
		cleanDatabase(t, drv, ctx)
		drv.Disconnect(ctx)
	})

	cleanDatabase(t, drv, ctx)

	return drv, ctx
}

func cleanDatabase(t *testing.T, drv *Driver, ctx context.Context) {
	t.Helper()

	rows, err := drv.db.QueryContext(ctx, `
		SELECT tablename FROM pg_tables
		WHERE schemaname = 'public' AND tablename != 'graviton_migrations'
	`)
	if err != nil {
		t.Fatalf("Failed to list tables: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("Failed to scan table name: %v", err)
		}
		tables = append(tables, table)
	}

	for _, table := range tables {
		if _, err := drv.db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table+" CASCADE"); err != nil {
			t.Fatalf("Failed to drop table %s: %v", table, err)
		}
	}

	if _, err := drv.db.ExecContext(ctx, "DELETE FROM "+MIGRATIONS_TABLE); err != nil {
		t.Fatalf("Failed to clean migrations table: %v", err)
	}
}

func Test_Driver_Connect(t *testing.T) {
	conf := &config.DatabaseConfig{
		ConnectionUrl: testDatabaseURL,
		DatabaseName:  testDatabaseName,
	}

	drv := New(conf)
	ctx := context.Background()

	err := drv.Connect(ctx)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer drv.Disconnect(ctx)

	if drv.db == nil {
		t.Error("Connect() did not set db")
	}
}

func Test_Driver_Commit(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	drv.db.ExecContext(ctx, "CREATE TABLE test (value TEXT)")

	if err := drv.BeginTx(ctx); err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if _, err := drv.tx.ExecContext(ctx, "INSERT INTO test (value) VALUES ($1)", "test"); err != nil {
		t.Fatalf("insert error = %v", err)
	}
	if err := drv.CommitTx(ctx); err != nil {
		t.Fatalf("CommitTx() error = %v", err)
	}
	if drv.HasOpenTx() {
		t.Error("HasOpenTx() = true after commit, want false")
	}

	var count int
	drv.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test").Scan(&count)
	if count != 1 {
		t.Errorf("COUNT(*) = %d, want 1 (transaction should commit)", count)
	}
}

func Test_Driver_Rollback(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	drv.db.ExecContext(ctx, "CREATE TABLE test (value TEXT)")

	if err := drv.BeginTx(ctx); err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	drv.tx.ExecContext(ctx, "INSERT INTO test (value) VALUES ($1)", "test")
	if err := drv.RollbackTx(ctx); err != nil {
		t.Fatalf("RollbackTx() error = %v", err)
	}
	if drv.HasOpenTx() {
		t.Error("HasOpenTx() = true after rollback, want false")
	}

	var count int
	drv.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test").Scan(&count)
	if count != 0 {
		t.Errorf("COUNT(*) = %d, want 0 (transaction should roll back)", count)
	}
}

func Test_Driver_Handle_LazilyBeginsTransaction(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	drv.db.ExecContext(ctx, "CREATE TABLE test (value TEXT)")

	handle := drv.Handle(ctx).(*Handle)
	if drv.HasOpenTx() {
		t.Fatal("HasOpenTx() = true before first operation, want false")
	}

	handle.Exec(&SQLQuery{Query: "INSERT INTO test (value) VALUES ($1)", Params: []any{"test"}})
	if !drv.HasOpenTx() {
		t.Fatal("handle operation did not lazily begin a transaction")
	}

	if err := drv.RollbackTx(ctx); err != nil {
		t.Fatalf("RollbackTx() error = %v", err)
	}

	var count int
	drv.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test").Scan(&count)
	if count != 0 {
		t.Errorf("COUNT(*) = %d, want 0 (uncommitted handle write should roll back)", count)
	}
}

func Test_Driver_SetAppliedMigrationsMetadata(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	migrations := []*migrationsmeta.MigrationMetadata{
		{
			Filename:  "20240101000000-one.migration.ts",
			Source:    "source1",
			AppliedAt: time.Now(),
		},
		{
			Filename:  "20240101000001-two.migration.ts",
			Source:    "source2",
			AppliedAt: time.Now(),
		},
	}

	err := drv.SetAppliedMigrationsMetadata(ctx, migrations)
	if err != nil {
		t.Fatalf("SetAppliedMigrationsMetadata() error = %v", err)
	}

	retrieved, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}

	if len(retrieved) != 2 {
		t.Errorf("GetAppliedMigrationsMetadata() returned %d migrations, want 2", len(retrieved))
	}
	if len(retrieved) > 0 && retrieved[0].Filename != migrations[0].Filename {
		t.Errorf("First migration filename = %s, want %s", retrieved[0].Filename, migrations[0].Filename)
	}
}

func Test_Driver_GetAppliedMigrationsMetadata_Empty(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	retrieved, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}

	if len(retrieved) != 0 {
		t.Errorf("GetAppliedMigrationsMetadata() returned %d migrations, want 0", len(retrieved))
	}
}
