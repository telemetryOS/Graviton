package mongodb

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"
)

// MongoDatabaseHandle is a database-scoped handle returned by the migration
// handle's db(name) accessor. It exposes the same collection(name) surface as
// the primary handle but binds operations to a sibling database on the same
// MongoDB client. Because it shares the client and the driver's active session
// context, cross-database reads and writes join the migration's transaction and
// rollback semantics. This only works across databases on a single cluster.
type MongoDatabaseHandle struct {
	ctx      context.Context
	driver   *Driver
	database *mongo.Database
}

func (h *MongoDatabaseHandle) Collection(name string) *Collection {
	return &Collection{ctx: h.ctx, driver: h.driver, database: h.database, name: name}
}
