package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"graviton/config"
	migrationsmeta "graviton/migrations-meta"
)

const (
	testDatabaseURL  = "root@tcp(localhost:3306)/graviton_test?parseTime=true"
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
		t.Skipf("MySQL not available: %v", err)
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

	rows, err := drv.db.QueryContext(ctx, "SHOW TABLES")
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
		if table != MIGRATIONS_TABLE {
			tables = append(tables, table)
		}
	}

	for _, table := range tables {
		if _, err := drv.db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
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
		t.Skipf("MySQL not available: %v", err)
	}
	defer drv.Disconnect(ctx)

	if drv.db == nil {
		t.Error("Connect() did not set db")
	}
}

func Test_Driver_WithTransaction_Success(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	drv.db.ExecContext(ctx, "CREATE TABLE test (value TEXT)")

	err := drv.WithTransaction(ctx, func(txCtx context.Context) error {
		_, err := drv.db.ExecContext(txCtx, "INSERT INTO test (value) VALUES (?)", "test")
		return err
	})

	if err != nil {
		t.Fatalf("WithTransaction() error = %v, want nil", err)
	}

	var count int
	drv.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test").Scan(&count)
	if count != 1 {
		t.Errorf("COUNT(*) = %d, want 1 (transaction should commit)", count)
	}
}

func Test_Driver_WithTransaction_ErrorReturned(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	drv.db.ExecContext(ctx, "CREATE TABLE test (value TEXT)")

	expectedErr := errors.New("test error")

	err := drv.WithTransaction(ctx, func(txCtx context.Context) error {
		drv.db.ExecContext(txCtx, "INSERT INTO test (value) VALUES (?)", "test")
		return expectedErr
	})

	if err == nil {
		t.Fatal("WithTransaction() error = nil, want error")
	}

	var count int
	drv.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test").Scan(&count)
	if count != 0 {
		t.Errorf("COUNT(*) = %d, want 0 (transaction should rollback)", count)
	}
}

func Test_Driver_WithTransaction_PanicRecovered(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	drv.db.ExecContext(ctx, "CREATE TABLE test (value TEXT)")

	err := drv.WithTransaction(ctx, func(txCtx context.Context) error {
		drv.db.ExecContext(txCtx, "INSERT INTO test (value) VALUES (?)", "test")
		panic(errors.New("panic error"))
	})

	if err == nil {
		t.Fatal("WithTransaction() error = nil, want error from recovered panic")
	}

	var count int
	drv.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test").Scan(&count)
	if count != 0 {
		t.Errorf("COUNT(*) = %d, want 0 (transaction should rollback after panic)", count)
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
}
