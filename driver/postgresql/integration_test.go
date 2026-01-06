package postgresql

import (
	"context"
	"errors"
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

	err := drv.WithTransaction(ctx, func(txCtx context.Context) error {
		tx := drv.getTxFromContext(txCtx)
		_, err := tx.ExecContext(txCtx, "INSERT INTO test_data (value) VALUES ($1)", "test")
		if err != nil {
			return err
		}

		if err := drv.SetAppliedMigrationsMetadata(txCtx, migrationsMeta); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		t.Fatalf("WithTransaction() error = %v, want nil", err)
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

func Test_Integration_TransactionAtomicity_DataFailure(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	drv.db.ExecContext(ctx, "CREATE TABLE test_data (value TEXT)")

	migrationsMeta := []*migrationsmeta.MigrationMetadata{
		{
			Filename:  "20240101000000-one.migration.ts",
			Source:    "source1",
			AppliedAt: time.Now(),
		},
	}

	expectedErr := errors.New("data insertion failed")

	err := drv.WithTransaction(ctx, func(txCtx context.Context) error {
		tx := drv.getTxFromContext(txCtx)
		tx.ExecContext(txCtx, "INSERT INTO test_data (value) VALUES ($1)", "test")

		if err := drv.SetAppliedMigrationsMetadata(txCtx, migrationsMeta); err != nil {
			return err
		}

		return expectedErr
	})

	if err == nil {
		t.Fatal("WithTransaction() error = nil, want error")
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

func Test_Integration_TransactionAtomicity_PanicRollback(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	drv.db.ExecContext(ctx, "CREATE TABLE test_data (value TEXT)")

	migrationsMeta := []*migrationsmeta.MigrationMetadata{
		{
			Filename:  "20240101000000-one.migration.ts",
			Source:    "source1",
			AppliedAt: time.Now(),
		},
	}

	err := drv.WithTransaction(ctx, func(txCtx context.Context) error {
		tx := drv.getTxFromContext(txCtx)
		tx.ExecContext(txCtx, "INSERT INTO test_data (value) VALUES ($1)", "test")

		if err := drv.SetAppliedMigrationsMetadata(txCtx, migrationsMeta); err != nil {
			return err
		}

		panic("simulated panic")
	})

	if err == nil {
		t.Fatal("WithTransaction() error = nil, want error from panic")
	}

	var count int
	drv.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM test_data").Scan(&count)
	if count != 0 {
		t.Errorf("COUNT(*) = %d, want 0 (data should be rolled back after panic)", count)
	}

	retrievedMeta, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}
	if len(retrievedMeta) != 0 {
		t.Errorf("GetAppliedMigrationsMetadata() returned %d migrations, want 0 (metadata should be rolled back after panic)", len(retrievedMeta))
	}
}
