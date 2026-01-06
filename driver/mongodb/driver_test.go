package mongodb

import (
	"context"
	"errors"
	"testing"
	"time"

	"graviton/config"
	migrationsmeta "graviton/migrations-meta"

	"go.mongodb.org/mongo-driver/bson"
)

const (
	testDatabaseURL  = "mongodb://localhost:27017"
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
		t.Skipf("MongoDB not available on localhost: %v", err)
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

	collections, err := drv.database.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		t.Fatalf("Failed to list collections: %v", err)
	}

	for _, coll := range collections {
		if err := drv.database.Collection(coll).Drop(ctx); err != nil {
			t.Fatalf("Failed to drop collection %s: %v", coll, err)
		}
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
		t.Skipf("MongoDB not available on localhost: %v", err)
	}
	defer drv.Disconnect(ctx)

	if drv.client == nil {
		t.Error("Connect() did not set client")
	}
	if drv.database == nil {
		t.Error("Connect() did not set database")
	}
}

func Test_Driver_WithTransaction_Success(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test")

	err := drv.WithTransaction(ctx, func(sessCtx context.Context) error {
		_, err := testColl.InsertOne(sessCtx, bson.M{"value": "test"})
		return err
	})

	if err != nil {
		t.Fatalf("WithTransaction() error = %v, want nil", err)
	}

	count, err := testColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() error = %v", err)
	}
	if count != 1 {
		t.Errorf("CountDocuments() = %d, want 1 (transaction should commit)", count)
	}
}

func Test_Driver_WithTransaction_ErrorReturned(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test")
	expectedErr := errors.New("test error")

	err := drv.WithTransaction(ctx, func(sessCtx context.Context) error {
		_, err := testColl.InsertOne(sessCtx, bson.M{"value": "test"})
		if err != nil {
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
		t.Errorf("CountDocuments() = %d, want 0 (transaction should rollback)", count)
	}
}

func Test_Driver_WithTransaction_PanicRecovered(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test")

	err := drv.WithTransaction(ctx, func(sessCtx context.Context) error {
		_, err := testColl.InsertOne(sessCtx, bson.M{"value": "test"})
		if err != nil {
			return err
		}
		panic(errors.New("panic error"))
	})

	if err == nil {
		t.Fatal("WithTransaction() error = nil, want error from recovered panic")
	}
	if err.Error() != "panic error" {
		t.Errorf("WithTransaction() error = %v, want 'panic error'", err)
	}

	count, err := testColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() error = %v", err)
	}
	if count != 0 {
		t.Errorf("CountDocuments() = %d, want 0 (transaction should rollback after panic)", count)
	}
}

func Test_Driver_WithTransaction_PanicString(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test")

	err := drv.WithTransaction(ctx, func(sessCtx context.Context) error {
		_, err := testColl.InsertOne(sessCtx, bson.M{"value": "test"})
		if err != nil {
			return err
		}
		panic("string panic")
	})

	if err == nil {
		t.Fatal("WithTransaction() error = nil, want error from recovered panic")
	}
	if err.Error() != "panic in transaction: string panic" {
		t.Errorf("WithTransaction() error = %v, want 'panic in transaction: string panic'", err)
	}

	count, err := testColl.CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() error = %v", err)
	}
	if count != 0 {
		t.Errorf("CountDocuments() = %d, want 0 (transaction should rollback after panic)", count)
	}
}

func Test_Driver_SetAppliedMigrationsMetadata(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	migrations := []*migrationsmeta.MigrationMetadata{
		{
			Filename:  "20231225010950-one.migration.ts",
			Source:    "source1",
			AppliedAt: time.Now(),
		},
		{
			Filename:  "20231225010956-two.migration.ts",
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

func Test_Driver_SetAppliedMigrationsMetadata_Empty(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	drv.database.Collection(MIGRATIONS_COLLECTION).InsertOne(ctx, bson.M{"filename": "test"})

	err := drv.SetAppliedMigrationsMetadata(ctx, []*migrationsmeta.MigrationMetadata{})
	if err != nil {
		t.Fatalf("SetAppliedMigrationsMetadata() error = %v", err)
	}

	retrieved, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}

	if len(retrieved) != 0 {
		t.Errorf("GetAppliedMigrationsMetadata() returned %d migrations, want 0", len(retrieved))
	}
}

func Test_Driver_GetAppliedMigrationsMetadata(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	retrieved, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}

	if len(retrieved) != 0 {
		t.Errorf("GetAppliedMigrationsMetadata() returned %d migrations, want 0 (empty database)", len(retrieved))
	}
}
