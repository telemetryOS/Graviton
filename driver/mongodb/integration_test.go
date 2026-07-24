package mongodb

import (
	"testing"
	"time"

	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"go.mongodb.org/mongo-driver/bson"
)

// These integration tests exercise a single driver's transaction as a unit:
// data writes and the tracking write it hosts commit or roll back together.
// The marker-last, cross-database orchestration is covered at the run level in
// the migrations package.

func Test_Integration_Transaction_Commit(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	handle := drv.Handle(ctx).(*MongoHandle)
	migrationsMeta := []*migrationsmeta.MigrationMetadata{
		{
			Filename:  "20231225010950-one.migration.ts",
			Source:    "source1",
			AppliedAt: time.Now(),
		},
	}

	handle.Collection("test_data").InsertOne(bson.M{"value": "test"})
	if err := drv.SetAppliedMigrationsMetadata(ctx, migrationsMeta); err != nil {
		t.Fatalf("SetAppliedMigrationsMetadata() error = %v", err)
	}
	if err := drv.CommitTx(ctx); err != nil {
		t.Fatalf("CommitTx() error = %v", err)
	}

	count, err := drv.database.Collection("test_data").CountDocuments(ctx, bson.M{})
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

func Test_Integration_Transaction_Rollback(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	handle := drv.Handle(ctx).(*MongoHandle)
	migrationsMeta := []*migrationsmeta.MigrationMetadata{
		{
			Filename:  "20231225010950-one.migration.ts",
			Source:    "source1",
			AppliedAt: time.Now(),
		},
	}

	handle.Collection("test_data").InsertOne(bson.M{"value": "test"})
	if err := drv.SetAppliedMigrationsMetadata(ctx, migrationsMeta); err != nil {
		t.Fatalf("SetAppliedMigrationsMetadata() error = %v", err)
	}
	if err := drv.RollbackTx(ctx); err != nil {
		t.Fatalf("RollbackTx() error = %v", err)
	}

	count, err := drv.database.Collection("test_data").CountDocuments(ctx, bson.M{})
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
