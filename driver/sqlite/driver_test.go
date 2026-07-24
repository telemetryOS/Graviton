package sqlite

import (
	"context"
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

func Test_Driver_Commit(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	drv.db.ExecContext(ctx, "CREATE TABLE test (value TEXT)")

	if err := drv.BeginTx(ctx); err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if _, err := drv.tx.ExecContext(ctx, "INSERT INTO test (value) VALUES (?)", "test"); err != nil {
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
	drv.tx.ExecContext(ctx, "INSERT INTO test (value) VALUES (?)", "test")
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

	handle.Exec(&SQLQuery{Query: "INSERT INTO test (value) VALUES (?)", Params: []any{"test"}})
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
