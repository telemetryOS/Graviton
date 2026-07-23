package mongodb

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Collection struct {
	ctx      context.Context
	driver   *Driver
	database *mongo.Database
	name     string
}

type InsertManyOptions = options.InsertManyOptions
type InsertOneOptions = options.InsertOneOptions
type FindOptions = options.FindOptions
type FindOneOptions = options.FindOneOptions
type UpdateOptions = options.UpdateOptions
type DeleteOptions = options.DeleteOptions

// opCtx returns the context used for collection operations. While a migration
// transaction is active the driver's session context is used so reads and
// writes — including those against sibling databases reached via db(name) —
// join the migration's transaction and rollback semantics. Outside of a
// transaction it falls back to the handle's context.
func (c *Collection) opCtx() context.Context {
	if c.driver.sessionCtx != nil {
		return c.driver.sessionCtx
	}
	return c.ctx
}

func (c *Collection) InsertMany(docs []any, options ...*InsertManyOptions) *mongo.InsertManyResult {
	result, err := c.database.Collection(c.name).InsertMany(c.opCtx(), docs, options...)
	if err != nil {
		panic(err)
	}
	return result
}

func (c *Collection) InsertOne(doc any, options ...*InsertOneOptions) *mongo.InsertOneResult {
	result, err := c.database.Collection(c.name).InsertOne(c.opCtx(), doc, options...)
	if err != nil {
		panic(err)
	}
	return result
}

func (c *Collection) Find(filter any, options ...*FindOptions) []map[string]any {
	ctx := c.opCtx()
	cur, err := c.database.Collection(c.name).Find(ctx, filter, options...)
	if err != nil {
		panic(err)
	}

	var results []map[string]any
	if err := cur.All(ctx, &results); err != nil {
		panic(err)
	}

	return results
}

func (c *Collection) FindOne(filter any, options ...*FindOneOptions) map[string]any {
	var result map[string]any
	err := c.database.Collection(c.name).FindOne(c.opCtx(), filter, options...).Decode(&result)
	if err != nil {
		// No match is a normal outcome for find-before-insert patterns; JS-land
		// receives null (a nil map) rather than a panic.
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil
		}
		panic(err)
	}
	return result
}

func (c *Collection) UpdateMany(filter any, update any, options ...*UpdateOptions) *mongo.UpdateResult {
	result, err := c.database.Collection(c.name).UpdateMany(c.opCtx(), filter, update, options...)
	if err != nil {
		panic(err)
	}
	return result
}

func (c *Collection) UpdateOne(filter any, update any, options ...*UpdateOptions) *mongo.UpdateResult {
	result, err := c.database.Collection(c.name).UpdateOne(c.opCtx(), filter, update, options...)
	if err != nil {
		panic(err)
	}
	return result
}

func (c *Collection) DeleteMany(filter any, options ...*DeleteOptions) *mongo.DeleteResult {
	result, err := c.database.Collection(c.name).DeleteMany(c.opCtx(), filter, options...)
	if err != nil {
		panic(err)
	}
	return result
}

func (c *Collection) DeleteOne(filter any, options ...*DeleteOptions) *mongo.DeleteResult {
	result, err := c.database.Collection(c.name).DeleteOne(c.opCtx(), filter, options...)
	if err != nil {
		panic(err)
	}
	return result
}
