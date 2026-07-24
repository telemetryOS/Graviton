package mongodb

import (
	"context"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// seedCollection writes one document directly (outside any Graviton
// transaction) so rename tests can arrange source/target state without opening
// a session on the driver under test.
func seedCollection(t *testing.T, drv *Driver, ctx context.Context, dbName, coll string) {
	t.Helper()
	_, err := drv.client.Database(dbName).Collection(coll).InsertOne(ctx, bson.M{"seeded": true})
	if err != nil {
		t.Fatalf("seed %s.%s: %v", dbName, coll, err)
	}
}

func dropDatabaseCleanup(t *testing.T, drv *Driver, ctx context.Context, dbName string) {
	t.Cleanup(func() {
		if err := drv.client.Database(dbName).Drop(ctx); err != nil {
			t.Logf("cleanup: drop database %s: %v", dbName, err)
		}
	})
}

func nonSystemCount(t *testing.T, drv *Driver, ctx context.Context, dbName string) int {
	t.Helper()
	names, err := drv.client.Database(dbName).ListCollectionNames(ctx, bson.M{})
	if err != nil {
		t.Fatalf("list collections %s: %v", dbName, err)
	}
	n := 0
	for _, name := range names {
		if strings.HasPrefix(name, "system.") {
			continue
		}
		n++
	}
	return n
}

func Test_Driver_RenameDatabase_MovesCollectionsAndDropsSource(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	target := testDatabaseName + "_renamed"
	dropDatabaseCleanup(t, drv, ctx, target)

	seedCollection(t, drv, ctx, testDatabaseName, "alpha")
	seedCollection(t, drv, ctx, testDatabaseName, "beta")

	if err := drv.RenameDatabase(ctx, target); err != nil {
		t.Fatalf("RenameDatabase() error = %v, want nil", err)
	}

	if got := nonSystemCount(t, drv, ctx, testDatabaseName); got != 0 {
		t.Errorf("source database still has %d collections after rename, want 0 (should be dropped)", got)
	}
	for _, coll := range []string{"alpha", "beta"} {
		count, err := drv.client.Database(target).Collection(coll).CountDocuments(ctx, bson.M{})
		if err != nil {
			t.Fatalf("count %s.%s: %v", target, coll, err)
		}
		if count != 1 {
			t.Errorf("target %s.%s has %d docs, want 1 (collection should have moved)", target, coll, count)
		}
	}

	// rename ran outside any transaction.
	if drv.HasOpenTx() {
		t.Error("RenameDatabase opened a transaction; it must run non-transactionally")
	}
}

func Test_Driver_RenameDatabase_TargetCollisionErrors(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	target := testDatabaseName + "_renamed"
	dropDatabaseCleanup(t, drv, ctx, target)

	seedCollection(t, drv, ctx, testDatabaseName, "alpha")
	// Pre-existing colliding collection in the target: rename must not clobber it.
	seedCollection(t, drv, ctx, target, "alpha")

	err := drv.RenameDatabase(ctx, target)
	if err == nil {
		t.Fatal("RenameDatabase() error = nil, want error for pre-existing target collection")
	}

	// The source must be left intact — no drop after a failed rename.
	if got := nonSystemCount(t, drv, ctx, testDatabaseName); got != 1 {
		t.Errorf("source database has %d collections after failed rename, want 1 (must not be dropped)", got)
	}
}

func Test_Driver_RenameDatabase_OpenTransactionErrors(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	target := testDatabaseName + "_renamed"

	handle := drv.Handle(ctx).(*MongoHandle)
	// A collection op lazily opens the driver's transaction.
	handle.Collection("alpha").InsertOne(bson.M{"n": 1})
	if !drv.HasOpenTx() {
		t.Fatal("expected an open transaction after a collection op")
	}

	err := drv.RenameDatabase(ctx, target)
	if err == nil {
		t.Fatal("RenameDatabase() error = nil, want error while a transaction is open")
	}
	if !strings.Contains(err.Error(), "transaction") {
		t.Errorf("RenameDatabase() error = %v, want it to mention the open transaction", err)
	}
}
