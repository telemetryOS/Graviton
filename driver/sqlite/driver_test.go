package sqlite

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/telemetryos/graviton/config"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"
)

func setupTestDriver(t *testing.T) (*Driver, context.Context) {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "graviton_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()

	conf := &config.DatabaseConfig{
		ConnectionUrl: "file:" + tmpFile.Name() + "?cache=shared&mode=rwc",
		DatabaseName:  "test",
	}

	drv := New(conf)
	ctx := context.Background()

	if err := drv.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	t.Cleanup(func() {
		drv.Disconnect(ctx)
		os.Remove(tmpFile.Name())
	})

	return drv, ctx
}

func Test_Driver_Connect(t *testing.T) {
	drv, ctx := setupTestDriver(t)
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
		tx := drv.getTxFromContext(txCtx)
		tx.ExecContext(txCtx, "INSERT INTO test (value) VALUES (?)", "test")
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
		tx := drv.getTxFromContext(txCtx)
		tx.ExecContext(txCtx, "INSERT INTO test (value) VALUES (?)", "test")
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
