package mongodb

import (
	"context"
	"github.com/telemetryos/graviton/driver/transaction"
	"go.mongodb.org/mongo-driver/bson"
)

// MongoHandle is the migration-facing handle bound to a single MongoDB
// database. The root handle exposes collection() directly (single-database
// projects) and returns a fresh MongoHandle from use(alias) for each configured
// database in a multi-database project.
type MongoHandle struct {
	ctx    context.Context
	driver *Driver
}

func (h *MongoHandle) Collection(name string) *Collection {
	return &Collection{ctx: h.ctx, driver: h.driver, database: h.driver.database, name: name}
}

// RunCommand executes a MongoDB command on this handle's database.
func (h *MongoHandle) RunCommand(name string, value any, parameters ...map[string]any) map[string]any {
	command := bson.D{{Key: name, Value: value}}
	for _, fields := range parameters {
		for key, value := range fields {
			command = append(command, bson.E{Key: key, Value: value})
		}
	}
	ctx := h.ctx
	if transaction.Bound(ctx) {
		ctx = h.driver.sessionCtx
	}
	var result map[string]any
	if err := h.driver.database.RunCommand(ctx, command).Decode(&result); err != nil {
		panic(err)
	}
	return result
}
