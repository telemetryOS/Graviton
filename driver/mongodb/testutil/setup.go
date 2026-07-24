package testutil

import (
	"context"
	"testing"

	"github.com/telemetryos/graviton/config"
	"github.com/telemetryos/graviton/driver/mongodb"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	TestDatabaseURL  = "mongodb://localhost:27017"
	TestDatabaseName = "graviton_test"
)

func SetupTestDriver(t *testing.T) (*mongodb.Driver, context.Context) {
	t.Helper()

	conf := &config.DatabaseConfig{
		ConnectionUrl: TestDatabaseURL,
		DatabaseName:  TestDatabaseName,
	}

	drv := mongodb.New(conf)
	ctx := context.Background()

	if err := drv.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	t.Cleanup(func() {
		drv.Disconnect(ctx)
	})

	return drv, ctx
}

func CleanTestDatabase(t *testing.T, drv *mongodb.Driver, ctx context.Context) {
	t.Helper()

	client := getClientFromDriver(t, drv)
	db := client.Database(TestDatabaseName)

	collections, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		t.Fatalf("Failed to list collections: %v", err)
	}

	for _, coll := range collections {
		if err := db.Collection(coll).Drop(ctx); err != nil {
			t.Fatalf("Failed to drop collection %s: %v", coll, err)
		}
	}
}

func GetTestCollection(t *testing.T, drv *mongodb.Driver, ctx context.Context, name string) *mongo.Collection {
	t.Helper()

	client := getClientFromDriver(t, drv)
	return client.Database(TestDatabaseName).Collection(name)
}

func getClientFromDriver(t *testing.T, drv *mongodb.Driver) *mongo.Client {
	t.Helper()

	ctx := context.Background()
	client, err := mongo.Connect(ctx, nil)
	if err != nil {
		client, err = mongo.Connect(ctx)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
	}

	t.Cleanup(func() {
		client.Disconnect(ctx)
	})

	return client
}
