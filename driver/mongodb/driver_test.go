package mongodb

import (
	"context"
	"testing"
	"time"

	"github.com/telemetryos/graviton/config"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"go.mongodb.org/mongo-driver/bson"
)

const (
	testDatabaseURL  = "mongodb://localhost:27017"
	testDatabaseName = "graviton_test"
)

func setupTestDriver(t *testing.T) (*Driver, context.Context) {
	t.Helper()

	conf := &config.DatabaseConfig{
		Name:          "primary",
		Kind:          config.DatabaseKindMongoDB,
		ConnectionUrl: testDatabaseURL,
		DatabaseName:  testDatabaseName,
	}

	drv := New(conf)
	ctx := context.Background()

	if err := drv.Connect(ctx); err != nil {
		t.Skipf("MongoDB not available on localhost: %v", err)
	}

	t.Cleanup(func() {
		// A handle operation lazily opens a transaction the collection tests
		// never commit; abort it before cleaning so the collection drops don't
		// contend with a lingering transaction.
		drv.RollbackTx(ctx)
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

func Test_Driver_Commit(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	handle := drv.Handle(ctx).(*MongoHandle)
	coll := handle.Collection("test")

	// The first collection operation lazily opens the driver's transaction.
	coll.InsertOne(bson.M{"value": "test"})
	if !drv.HasOpenTx() {
		t.Fatal("handle operation did not lazily begin a transaction")
	}
	if err := drv.CommitTx(ctx); err != nil {
		t.Fatalf("CommitTx() error = %v", err)
	}
	if drv.HasOpenTx() {
		t.Error("HasOpenTx() = true after commit, want false")
	}

	count, err := drv.database.Collection("test").CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() error = %v", err)
	}
	if count != 1 {
		t.Errorf("CountDocuments() = %d, want 1 (transaction should commit)", count)
	}
}

func Test_Driver_Rollback(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	handle := drv.Handle(ctx).(*MongoHandle)
	coll := handle.Collection("test")

	coll.InsertOne(bson.M{"value": "test"})
	if err := drv.RollbackTx(ctx); err != nil {
		t.Fatalf("RollbackTx() error = %v", err)
	}
	if drv.HasOpenTx() {
		t.Error("HasOpenTx() = true after rollback, want false")
	}

	count, err := drv.database.Collection("test").CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() error = %v", err)
	}
	if count != 0 {
		t.Errorf("CountDocuments() = %d, want 0 (transaction should roll back)", count)
	}
}

func Test_Driver_SessionReusedAcrossTransactions(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	handle := drv.Handle(ctx).(*MongoHandle)

	handle.Collection("test").InsertOne(bson.M{"n": 1})
	firstSession := drv.session
	if err := drv.CommitTx(ctx); err != nil {
		t.Fatalf("CommitTx() error = %v", err)
	}

	handle.Collection("test").InsertOne(bson.M{"n": 2})
	if drv.session != firstSession {
		t.Error("second transaction used a new session; one session per database per run expected")
	}
	if err := drv.CommitTx(ctx); err != nil {
		t.Fatalf("CommitTx() error = %v", err)
	}

	count, _ := drv.database.Collection("test").CountDocuments(ctx, bson.M{})
	if count != 2 {
		t.Errorf("CountDocuments() = %d, want 2", count)
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
