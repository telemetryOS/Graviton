package mongodb

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func Test_Collection_InsertOne_Success(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	handle := drv.Handle(ctx).(*MongoHandle)
	coll := handle.Collection("test")

	result := coll.InsertOne(bson.M{"value": "test"})
	if result.InsertedID == nil {
		t.Error("InsertOne() returned nil InsertedID")
	}
}

func Test_Collection_InsertMany_Success(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	handle := drv.Handle(ctx).(*MongoHandle)
	coll := handle.Collection("test")

	docs := []any{
		bson.M{"value": "test1"},
		bson.M{"value": "test2"},
	}

	result := coll.InsertMany(docs)
	if len(result.InsertedIDs) != 2 {
		t.Errorf("InsertMany() returned %d InsertedIDs, want 2", len(result.InsertedIDs))
	}
}

func Test_Collection_Find_Success(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test")
	testColl.InsertOne(ctx, bson.M{"value": "test1"})
	testColl.InsertOne(ctx, bson.M{"value": "test2"})

	handle := drv.Handle(ctx).(*MongoHandle)
	coll := handle.Collection("test")

	results := coll.Find(bson.M{})
	if len(results) != 2 {
		t.Errorf("Find() returned %d results, want 2", len(results))
	}
}

func Test_Collection_FindOne_Success(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test")
	testColl.InsertOne(ctx, bson.M{"value": "test"})

	handle := drv.Handle(ctx).(*MongoHandle)
	coll := handle.Collection("test")

	result := coll.FindOne(bson.M{"value": "test"})
	if result == nil {
		t.Error("FindOne() returned nil")
	}
	if result["value"] != "test" {
		t.Errorf("FindOne() value = %v, want 'test'", result["value"])
	}
}

func Test_Collection_FindOne_DecodeError(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	handle := drv.Handle(ctx).(*MongoHandle)
	coll := handle.Collection("test")

	defer func() {
		if r := recover(); r == nil {
			t.Error("FindOne() should panic on ErrNoDocuments, but did not")
		} else if err, ok := r.(error); ok && err != mongo.ErrNoDocuments {
			t.Errorf("FindOne() panic = %v, want ErrNoDocuments", err)
		}
	}()

	coll.FindOne(bson.M{"nonexistent": "value"})
}

func Test_Collection_UpdateOne_Success(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test")
	testColl.InsertOne(ctx, bson.M{"value": "test"})

	handle := drv.Handle(ctx).(*MongoHandle)
	coll := handle.Collection("test")

	result := coll.UpdateOne(bson.M{"value": "test"}, bson.M{"$set": bson.M{"value": "updated"}})
	if result.ModifiedCount != 1 {
		t.Errorf("UpdateOne() ModifiedCount = %d, want 1", result.ModifiedCount)
	}
}

func Test_Collection_UpdateMany_Success(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test")
	testColl.InsertOne(ctx, bson.M{"value": "test"})
	testColl.InsertOne(ctx, bson.M{"value": "test"})

	handle := drv.Handle(ctx).(*MongoHandle)
	coll := handle.Collection("test")

	result := coll.UpdateMany(bson.M{"value": "test"}, bson.M{"$set": bson.M{"value": "updated"}})
	if result.ModifiedCount != 2 {
		t.Errorf("UpdateMany() ModifiedCount = %d, want 2", result.ModifiedCount)
	}
}

func Test_Collection_DeleteOne_Success(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test")
	testColl.InsertOne(ctx, bson.M{"value": "test"})

	handle := drv.Handle(ctx).(*MongoHandle)
	coll := handle.Collection("test")

	result := coll.DeleteOne(bson.M{"value": "test"})
	if result.DeletedCount != 1 {
		t.Errorf("DeleteOne() DeletedCount = %d, want 1", result.DeletedCount)
	}
}

func Test_Collection_DeleteMany_Success(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test")
	testColl.InsertOne(ctx, bson.M{"value": "test"})
	testColl.InsertOne(ctx, bson.M{"value": "test"})

	handle := drv.Handle(ctx).(*MongoHandle)
	coll := handle.Collection("test")

	result := coll.DeleteMany(bson.M{"value": "test"})
	if result.DeletedCount != 2 {
		t.Errorf("DeleteMany() DeletedCount = %d, want 2", result.DeletedCount)
	}
}

func Test_Collection_InsertOne_InvalidDocument(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test")
	testColl.InsertOne(ctx, bson.M{"_id": 1, "value": "test"})

	handle := drv.Handle(ctx).(*MongoHandle)
	coll := handle.Collection("test")

	defer func() {
		if r := recover(); r == nil {
			t.Error("InsertOne() should panic on duplicate key error, but did not")
		}
	}()

	coll.InsertOne(bson.M{"_id": 1, "value": "duplicate"})
}

func Test_Collection_Find_WithOptions(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	testColl := drv.database.Collection("test")
	testColl.InsertOne(ctx, bson.M{"value": 1})
	testColl.InsertOne(ctx, bson.M{"value": 2})
	testColl.InsertOne(ctx, bson.M{"value": 3})

	handle := drv.Handle(ctx).(*MongoHandle)
	coll := handle.Collection("test")

	opts := options.Find().SetLimit(2)
	results := coll.Find(bson.M{}, opts)

	if len(results) != 2 {
		t.Errorf("Find() with limit returned %d results, want 2", len(results))
	}
}
