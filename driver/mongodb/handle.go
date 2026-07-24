package mongodb

import (
	"context"
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
