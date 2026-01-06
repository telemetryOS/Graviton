package mongodb

import (
	"context"
	"errors"
	"testing"
	"time"

	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"go.mongodb.org/mongo-driver/bson"
)

func Test_Integration_TransactionAtomicity_Success(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test_data")
	migrationsMeta := []*migrationsmeta.MigrationMetadata{
		{
			Filename:  "20231225010950-one.migration.ts",
			Source:    "source1",
			AppliedAt: time.Now(),
		},
	}

	err := drv.WithTransaction(ctx, func(sessCtx context.Context) error {
		_, err := testColl.InsertOne(sessCtx, bson.M{"value": "test"})
		if err != nil {
			return err
		}

		if err := drv.SetAppliedMigrationsMetadata(sessCtx, migrationsMeta); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		t.Fatalf("WithTransaction() error = %v, want nil", err)
	}

	count, err := testColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() error = %v", err)
	}
	if count != 1 {
		t.Errorf("CountDocuments() = %d, want 1 (data should be committed)", count)
	}

	retrievedMeta, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}
	if len(retrievedMeta) != 1 {
		t.Errorf("GetAppliedMigrationsMetadata() returned %d migrations, want 1 (metadata should be committed)", len(retrievedMeta))
	}
}

func Test_Integration_TransactionAtomicity_DataFailure(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test_data")
	migrationsMeta := []*migrationsmeta.MigrationMetadata{
		{
			Filename:  "20231225010950-one.migration.ts",
			Source:    "source1",
			AppliedAt: time.Now(),
		},
	}

	expectedErr := errors.New("data insertion failed")

	err := drv.WithTransaction(ctx, func(sessCtx context.Context) error {
		_, err := testColl.InsertOne(sessCtx, bson.M{"value": "test"})
		if err != nil {
			return err
		}

		if err := drv.SetAppliedMigrationsMetadata(sessCtx, migrationsMeta); err != nil {
			return err
		}

		return expectedErr
	})

	if err == nil {
		t.Fatal("WithTransaction() error = nil, want error")
	}
	if !errors.Is(err, expectedErr) && err.Error() != expectedErr.Error() {
		t.Errorf("WithTransaction() error = %v, want %v", err, expectedErr)
	}

	count, err := testColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() error = %v", err)
	}
	if count != 0 {
		t.Errorf("CountDocuments() = %d, want 0 (data should be rolled back)", count)
	}

	retrievedMeta, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}
	if len(retrievedMeta) != 0 {
		t.Errorf("GetAppliedMigrationsMetadata() returned %d migrations, want 0 (metadata should be rolled back)", len(retrievedMeta))
	}
}

func Test_Integration_TransactionAtomicity_MetadataFailure(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test_data")

	migrationsMeta := []*migrationsmeta.MigrationMetadata{
		{
			Filename:  "invalid-filename.txt",
			Source:    "source1",
			AppliedAt: time.Now(),
		},
	}

	err := drv.WithTransaction(ctx, func(sessCtx context.Context) error {
		_, err := testColl.InsertOne(sessCtx, bson.M{"value": "test"})
		if err != nil {
			return err
		}

		if err := drv.SetAppliedMigrationsMetadata(sessCtx, migrationsMeta); err != nil {
			return err
		}

		return errors.New("simulated failure after metadata")
	})

	if err == nil {
		t.Fatal("WithTransaction() error = nil, want error")
	}

	count, err := testColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() error = %v", err)
	}
	if count != 0 {
		t.Errorf("CountDocuments() = %d, want 0 (data should be rolled back)", count)
	}

	retrievedMeta, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}
	if len(retrievedMeta) != 0 {
		t.Errorf("GetAppliedMigrationsMetadata() returned %d migrations, want 0 (metadata should be rolled back)", len(retrievedMeta))
	}
}

func Test_Integration_TransactionAtomicity_MultipleMigrations(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test_data")

	migration1 := &migrationsmeta.MigrationMetadata{
		Filename:  "20231225010950-one.migration.ts",
		Source:    "source1",
		AppliedAt: time.Now(),
	}
	migration2 := &migrationsmeta.MigrationMetadata{
		Filename:  "20231225010956-two.migration.ts",
		Source:    "source2",
		AppliedAt: time.Now(),
	}

	err := drv.WithTransaction(ctx, func(sessCtx context.Context) error {
		_, err := testColl.InsertOne(sessCtx, bson.M{"migration": "one"})
		if err != nil {
			return err
		}

		if err := drv.SetAppliedMigrationsMetadata(sessCtx, []*migrationsmeta.MigrationMetadata{migration1}); err != nil {
			return err
		}

		_, err = testColl.InsertOne(sessCtx, bson.M{"migration": "two"})
		if err != nil {
			return err
		}

		if err := drv.SetAppliedMigrationsMetadata(sessCtx, []*migrationsmeta.MigrationMetadata{migration1, migration2}); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		t.Fatalf("WithTransaction() error = %v, want nil", err)
	}

	count, err := testColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() error = %v", err)
	}
	if count != 2 {
		t.Errorf("CountDocuments() = %d, want 2 (both migrations should be committed)", count)
	}

	retrievedMeta, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}
	if len(retrievedMeta) != 2 {
		t.Errorf("GetAppliedMigrationsMetadata() returned %d migrations, want 2", len(retrievedMeta))
	}
}

func Test_Integration_TransactionAtomicity_PanicRollback(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test_data")
	migrationsMeta := []*migrationsmeta.MigrationMetadata{
		{
			Filename:  "20231225010950-one.migration.ts",
			Source:    "source1",
			AppliedAt: time.Now(),
		},
	}

	err := drv.WithTransaction(ctx, func(sessCtx context.Context) error {
		_, err := testColl.InsertOne(sessCtx, bson.M{"value": "test"})
		if err != nil {
			return err
		}

		if err := drv.SetAppliedMigrationsMetadata(sessCtx, migrationsMeta); err != nil {
			return err
		}

		panic("simulated panic")
	})

	if err == nil {
		t.Fatal("WithTransaction() error = nil, want error from panic")
	}

	count, err := testColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() error = %v", err)
	}
	if count != 0 {
		t.Errorf("CountDocuments() = %d, want 0 (data should be rolled back after panic)", count)
	}

	retrievedMeta, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}
	if len(retrievedMeta) != 0 {
		t.Errorf("GetAppliedMigrationsMetadata() returned %d migrations, want 0 (metadata should be rolled back after panic)", len(retrievedMeta))
	}
}
