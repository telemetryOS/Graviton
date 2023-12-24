package mongodb

import (
	"context"

	"rogchap.com/v8go"
)

type Collection struct {
	ctx    context.Context
	driver *Driver
	name   string
}

func (c *Collection) Find(filter any) []map[string]any {
	cur, err := c.driver.database.Collection(c.name).Find(c.ctx, filter)
	if err != nil {
		panic(err)
	}

	var results []map[string]any
	if err := cur.All(c.ctx, &results); err != nil {
		panic(err)
	}

	return results
}

func (c *Collection) V8Value(v8Iso *v8go.Isolate) *v8go.Value {
	return nil
}
