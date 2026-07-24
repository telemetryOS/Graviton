package postgresql

import (
	"testing"
	"time"

	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"
)

func Test_Integration_TransactionAtomicity_Success(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	drv.db.ExecContext(ctx, "CREATE TABLE test_data (value TEXT)")

	migrationsMeta := []*migrationsmeta.MigrationMetadata{
		{
			Filename:  "20240101000000-one.migration.ts",
			Source:    "source1",
			AppliedAt: time.Now(),
		},
	}

	if err := drv.BeginTx(ctx); err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if _, err := drv.tx.ExecContext(ctx, "INSERT INTO test_data (value) VALUES ($1)", "test"); err != nil {
		t.Fatalf("insert error = %v", err)
	}
	if err := drv.SetAppliedMigrationsMetadata(ctx, migrationsMeta); err != nil {
		t.Fatalf("SetAppliedMigrationsMetadata() error = %v", err)
	}
	if err := drv.CommitTx(ctx); err != nil {
		t.Fatalf("CommitTx() error = %v", err)
	}

	var count int
	drv.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_data").Scan(&count)
	if count != 1 {
		t.Errorf("COUNT(*) = %d, want 1 (data should be committed)", count)
	}

	retrievedMeta, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}
	if len(retrievedMeta) != 1 {
		t.Errorf("GetAppliedMigrationsMetadata() returned %d migrations, want 1", len(retrievedMeta))
	}
}

func Test_Integration_TransactionAtomicity_Rollback(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	drv.db.ExecContext(ctx, "CREATE TABLE test_data (value TEXT)")

	migrationsMeta := []*migrationsmeta.MigrationMetadata{
		{
			Filename:  "20240101000000-one.migration.ts",
			Source:    "source1",
			AppliedAt: time.Now(),
		},
	}

	if err := drv.BeginTx(ctx); err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	drv.tx.ExecContext(ctx, "INSERT INTO test_data (value) VALUES ($1)", "test")
	if err := drv.SetAppliedMigrationsMetadata(ctx, migrationsMeta); err != nil {
		t.Fatalf("SetAppliedMigrationsMetadata() error = %v", err)
	}
	if err := drv.RollbackTx(ctx); err != nil {
		t.Fatalf("RollbackTx() error = %v", err)
	}

	var count int
	drv.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_data").Scan(&count)
	if count != 0 {
		t.Errorf("COUNT(*) = %d, want 0 (data should be rolled back)", count)
	}

	retrievedMeta, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}
	if len(retrievedMeta) != 0 {
		t.Errorf("GetAppliedMigrationsMetadata() returned %d migrations, want 0 (metadata should be rolled back)", len(retrievedMeta))
	}
}
