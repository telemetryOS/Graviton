package mongodb

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/telemetryos/graviton/config"

	"go.mongodb.org/mongo-driver/bson"
)

const (
	testSiblingDatabaseName  = "graviton_test_sibling"
	testSiblingDatabaseAlias = "legacy"
)

// testSiblingDatabaseConfig is the configured [[databases]] entry that aliases
// `legacy` to the physical sibling database used by the cross-database tests,
// on the same connection as the primary test database.
func testSiblingDatabaseConfig() *config.DatabaseConfig {
	return &config.DatabaseConfig{
		Name:          testSiblingDatabaseAlias,
		Kind:          config.DatabaseKindMongoDB,
		ConnectionUrl: testDatabaseURL,
		DatabaseName:  testSiblingDatabaseName,
	}
}

func cleanNamedDatabase(t *testing.T, drv *Driver, ctx context.Context, name string) {
	t.Helper()

	db := drv.client.Database(name)
	collections, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		t.Fatalf("Failed to list collections in %s: %v", name, err)
	}

	for _, coll := range collections {
		if err := db.Collection(coll).Drop(ctx); err != nil {
			t.Fatalf("Failed to drop collection %s.%s: %v", name, coll, err)
		}
	}
}

func setupSiblingDatabase(t *testing.T, drv *Driver, ctx context.Context) {
	t.Helper()

	cleanNamedDatabase(t, drv, ctx, testSiblingDatabaseName)
	t.Cleanup(func() {
		cleanNamedDatabase(t, drv, ctx, testSiblingDatabaseName)
	})
}

func Test_DatabaseHandle_CrossDatabaseReadWrite(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	setupSiblingDatabase(t, drv, ctx)

	sibling := drv.client.Database(testSiblingDatabaseName)
	if _, err := sibling.Collection("legacy").InsertOne(ctx, bson.M{"value": "from-legacy"}); err != nil {
		t.Fatalf("Failed to seed sibling database: %v", err)
	}

	handle := drv.Handle(ctx).(*MongoHandle)

	legacyDocs := handle.Db(testSiblingDatabaseName).Collection("legacy").Find(bson.M{})
	if len(legacyDocs) != 1 {
		t.Fatalf("Find() on sibling database returned %d docs, want 1", len(legacyDocs))
	}
	if legacyDocs[0]["value"] != "from-legacy" {
		t.Errorf("sibling doc value = %v, want 'from-legacy'", legacyDocs[0]["value"])
	}

	handle.Db(testSiblingDatabaseName).Collection("target").InsertOne(bson.M{"value": "written"})

	count, err := sibling.Collection("target").CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() error = %v", err)
	}
	if count != 1 {
		t.Errorf("sibling target count = %d, want 1", count)
	}
}

func Test_DatabaseHandle_TargetsDistinctDatabase(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	setupSiblingDatabase(t, drv, ctx)

	handle := drv.Handle(ctx).(*MongoHandle)

	handle.Db(testSiblingDatabaseName).Collection("shared").InsertOne(bson.M{"where": "sibling"})
	handle.Collection("shared").InsertOne(bson.M{"where": "primary"})

	primaryCount, err := drv.database.Collection("shared").CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() primary error = %v", err)
	}
	siblingCount, err := drv.client.Database(testSiblingDatabaseName).Collection("shared").CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() sibling error = %v", err)
	}

	if primaryCount != 1 {
		t.Errorf("primary shared count = %d, want 1", primaryCount)
	}
	if siblingCount != 1 {
		t.Errorf("sibling shared count = %d, want 1", siblingCount)
	}
}

func Test_DatabaseHandle_CrossDatabaseTransaction_Commit(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	setupSiblingDatabase(t, drv, ctx)

	if err := drv.database.CreateCollection(ctx, "primary_coll"); err != nil {
		t.Fatalf("CreateCollection(primary) error = %v", err)
	}
	if err := drv.client.Database(testSiblingDatabaseName).CreateCollection(ctx, "sibling_coll"); err != nil {
		t.Fatalf("CreateCollection(sibling) error = %v", err)
	}

	handle := drv.Handle(ctx).(*MongoHandle)

	err := drv.WithTransaction(ctx, func(sessCtx context.Context) error {
		handle.Collection("primary_coll").InsertOne(bson.M{"v": 1})
		handle.Db(testSiblingDatabaseName).Collection("sibling_coll").InsertOne(bson.M{"v": 2})
		return nil
	})
	if err != nil {
		t.Fatalf("WithTransaction() error = %v, want nil", err)
	}

	primaryCount, _ := drv.database.Collection("primary_coll").CountDocuments(ctx, bson.M{})
	siblingCount, _ := drv.client.Database(testSiblingDatabaseName).Collection("sibling_coll").CountDocuments(ctx, bson.M{})

	if primaryCount != 1 {
		t.Errorf("primary count = %d, want 1 (should commit)", primaryCount)
	}
	if siblingCount != 1 {
		t.Errorf("sibling count = %d, want 1 (should commit)", siblingCount)
	}
}

func Test_DatabaseHandle_CrossDatabaseTransaction_Rollback(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	setupSiblingDatabase(t, drv, ctx)

	if err := drv.database.CreateCollection(ctx, "primary_coll"); err != nil {
		t.Fatalf("CreateCollection(primary) error = %v", err)
	}
	if err := drv.client.Database(testSiblingDatabaseName).CreateCollection(ctx, "sibling_coll"); err != nil {
		t.Fatalf("CreateCollection(sibling) error = %v", err)
	}

	handle := drv.Handle(ctx).(*MongoHandle)

	err := drv.WithTransaction(ctx, func(sessCtx context.Context) error {
		handle.Collection("primary_coll").InsertOne(bson.M{"v": 1})
		handle.Db(testSiblingDatabaseName).Collection("sibling_coll").InsertOne(bson.M{"v": 2})
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatal("WithTransaction() error = nil, want error")
	}

	primaryCount, _ := drv.database.Collection("primary_coll").CountDocuments(ctx, bson.M{})
	siblingCount, _ := drv.client.Database(testSiblingDatabaseName).Collection("sibling_coll").CountDocuments(ctx, bson.M{})

	if primaryCount != 0 {
		t.Errorf("primary count = %d, want 0 (should roll back)", primaryCount)
	}
	if siblingCount != 0 {
		t.Errorf("sibling count = %d, want 0 (cross-database write should roll back)", siblingCount)
	}
}

// resolveSiblingDatabaseName is a pure resolution over the configured databases
// and needs no live MongoDB, so these cases run unconditionally.
func Test_ResolveSiblingDatabaseName_ResolvesAliasToPhysicalName(t *testing.T) {
	primary := &config.DatabaseConfig{
		Name:          "primary",
		Kind:          config.DatabaseKindMongoDB,
		ConnectionUrl: testDatabaseURL,
		DatabaseName:  testDatabaseName,
	}
	drv := New(primary, []*config.DatabaseConfig{primary, testSiblingDatabaseConfig()})

	name, err := drv.resolveSiblingDatabaseName(testSiblingDatabaseAlias)
	if err != nil {
		t.Fatalf("resolveSiblingDatabaseName() error = %v, want nil", err)
	}
	if name != testSiblingDatabaseName {
		t.Errorf("resolved physical name = %q, want %q", name, testSiblingDatabaseName)
	}
}

func Test_ResolveSiblingDatabaseName_UnknownAlias(t *testing.T) {
	primary := &config.DatabaseConfig{
		Name:          "primary",
		Kind:          config.DatabaseKindMongoDB,
		ConnectionUrl: testDatabaseURL,
		DatabaseName:  testDatabaseName,
	}
	drv := New(primary, []*config.DatabaseConfig{primary})

	_, err := drv.resolveSiblingDatabaseName("does_not_exist")
	if err == nil {
		t.Fatal("resolveSiblingDatabaseName() error = nil, want error for unknown alias")
	}
	if !strings.Contains(err.Error(), "does_not_exist") || !strings.Contains(err.Error(), "primary") {
		t.Errorf("error = %q, want mention of the alias and the configured databases", err.Error())
	}
}

func Test_ResolveSiblingDatabaseName_NonMongoAlias(t *testing.T) {
	primary := &config.DatabaseConfig{
		Name:          "primary",
		Kind:          config.DatabaseKindMongoDB,
		ConnectionUrl: testDatabaseURL,
		DatabaseName:  testDatabaseName,
	}
	analytics := &config.DatabaseConfig{
		Name:          "analytics",
		Kind:          config.DatabaseKindPostgreSQL,
		ConnectionUrl: "postgres://localhost:5432/analytics",
		DatabaseName:  "analytics",
	}
	drv := New(primary, []*config.DatabaseConfig{primary, analytics})

	_, err := drv.resolveSiblingDatabaseName("analytics")
	if err == nil {
		t.Fatal("resolveSiblingDatabaseName() error = nil, want error for non-mongodb alias")
	}
	if !strings.Contains(err.Error(), "mongodb") {
		t.Errorf("error = %q, want mention that sibling() only reaches mongodb databases", err.Error())
	}
}

func Test_ResolveSiblingDatabaseName_DifferentCluster(t *testing.T) {
	primary := &config.DatabaseConfig{
		Name:          "primary",
		Kind:          config.DatabaseKindMongoDB,
		ConnectionUrl: testDatabaseURL,
		DatabaseName:  testDatabaseName,
	}
	otherCluster := &config.DatabaseConfig{
		Name:          "remote",
		Kind:          config.DatabaseKindMongoDB,
		ConnectionUrl: "mongodb://other-host:27017",
		DatabaseName:  "remote_db",
	}
	drv := New(primary, []*config.DatabaseConfig{primary, otherCluster})

	_, err := drv.resolveSiblingDatabaseName("remote")
	if err == nil {
		t.Fatal("resolveSiblingDatabaseName() error = nil, want error for different-cluster alias")
	}
	if !strings.Contains(err.Error(), "same cluster") {
		t.Errorf("error = %q, want mention that sibling() only reaches the same cluster", err.Error())
	}
}

func Test_SiblingHandle_ResolvesAliasAndReadsWrites(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	setupSiblingDatabase(t, drv, ctx)

	sibling := drv.client.Database(testSiblingDatabaseName)
	if _, err := sibling.Collection("legacy").InsertOne(ctx, bson.M{"value": "from-legacy"}); err != nil {
		t.Fatalf("Failed to seed sibling database: %v", err)
	}

	handle := drv.Handle(ctx).(*MongoHandle)

	legacyDocs := handle.Sibling(testSiblingDatabaseAlias).Collection("legacy").Find(bson.M{})
	if len(legacyDocs) != 1 {
		t.Fatalf("sibling(alias) Find() returned %d docs, want 1", len(legacyDocs))
	}
	if legacyDocs[0]["value"] != "from-legacy" {
		t.Errorf("sibling doc value = %v, want 'from-legacy'", legacyDocs[0]["value"])
	}

	handle.Sibling(testSiblingDatabaseAlias).Collection("target").InsertOne(bson.M{"value": "written"})

	count, err := sibling.Collection("target").CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("CountDocuments() error = %v", err)
	}
	if count != 1 {
		t.Errorf("sibling target count = %d, want 1", count)
	}
}

func Test_SiblingHandle_UnknownAliasPanics(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	handle := drv.Handle(ctx).(*MongoHandle)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("sibling(unknown) did not panic, want panic")
		}
		if err, ok := r.(error); !ok || !strings.Contains(err.Error(), "not_configured") {
			t.Errorf("panic = %v, want error mentioning the unknown alias", r)
		}
	}()

	handle.Sibling("not_configured")
}

func Test_SiblingHandle_CrossDatabaseTransaction_Rollback(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	setupSiblingDatabase(t, drv, ctx)

	if err := drv.database.CreateCollection(ctx, "primary_coll"); err != nil {
		t.Fatalf("CreateCollection(primary) error = %v", err)
	}
	if err := drv.client.Database(testSiblingDatabaseName).CreateCollection(ctx, "sibling_coll"); err != nil {
		t.Fatalf("CreateCollection(sibling) error = %v", err)
	}

	handle := drv.Handle(ctx).(*MongoHandle)

	err := drv.WithTransaction(ctx, func(sessCtx context.Context) error {
		handle.Collection("primary_coll").InsertOne(bson.M{"v": 1})
		handle.Sibling(testSiblingDatabaseAlias).Collection("sibling_coll").InsertOne(bson.M{"v": 2})
		return errors.New("force rollback")
	})
	if err == nil {
		t.Fatal("WithTransaction() error = nil, want error")
	}

	primaryCount, _ := drv.database.Collection("primary_coll").CountDocuments(ctx, bson.M{})
	siblingCount, _ := drv.client.Database(testSiblingDatabaseName).Collection("sibling_coll").CountDocuments(ctx, bson.M{})

	if primaryCount != 0 {
		t.Errorf("primary count = %d, want 0 (should roll back)", primaryCount)
	}
	if siblingCount != 0 {
		t.Errorf("sibling count = %d, want 0 (cross-database sibling write should roll back)", siblingCount)
	}
}
