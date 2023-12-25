package mongodb

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Collection struct {
	ctx    context.Context
	driver *Driver
	name   string
}

type InsertManyOptions = options.InsertManyOptions
type InsertOneOptions = options.InsertOneOptions
type FindOptions = options.FindOptions
type FindOneOptions = options.FindOneOptions
type UpdateOptions = options.UpdateOptions
type DeleteOptions = options.DeleteOptions

func (c *Collection) InsertMany(docs []any, options ...*InsertManyOptions) *mongo.InsertManyResult {
	result, err := c.driver.database.Collection(c.name).InsertMany(c.ctx, docs, options...)
	if err != nil {
		panic(err)
	}
	return result
}

func (c *Collection) InsertOne(doc any, options ...*InsertOneOptions) *mongo.InsertOneResult {
	result, err := c.driver.database.Collection(c.name).InsertOne(c.ctx, doc, options...)
	if err != nil {
		panic(err)
	}
	return result
}

func (c *Collection) Find(filter any, options ...*FindOptions) []map[string]any {
	cur, err := c.driver.database.Collection(c.name).Find(c.ctx, filter, options...)
	if err != nil {
		panic(err)
	}

	var results []map[string]any
	if err := cur.All(c.ctx, &results); err != nil {
		panic(err)
	}

	return results
}

func (c *Collection) FindOne(filter any, options ...*FindOneOptions) map[string]any {
	var result map[string]any
	c.driver.database.Collection(c.name).FindOne(c.ctx, filter, options...).Decode(&result)
	return result
}

func (c *Collection) UpdateMany(filter any, update any, options ...*UpdateOptions) *mongo.UpdateResult {
	result, err := c.driver.database.Collection(c.name).UpdateMany(c.ctx, filter, update, options...)
	if err != nil {
		panic(err)
	}
	return result
}

func (c *Collection) UpdateOne(filter any, update any, options ...*UpdateOptions) *mongo.UpdateResult {
	result, err := c.driver.database.Collection(c.name).UpdateOne(c.ctx, filter, update, options...)
	if err != nil {
		panic(err)
	}
	return result
}

func (c *Collection) DeleteMany(filter any, options ...*DeleteOptions) *mongo.DeleteResult {
	result, err := c.driver.database.Collection(c.name).DeleteMany(c.ctx, filter, options...)
	if err != nil {
		panic(err)
	}
	return result
}

func (c *Collection) DeleteOne(filter any, options ...*DeleteOptions) *mongo.DeleteResult {
	result, err := c.driver.database.Collection(c.name).DeleteOne(c.ctx, filter, options...)
	if err != nil {
		panic(err)
	}
	return result
}
