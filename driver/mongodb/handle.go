package mongodb

import (
	"context"
)

type MongoHandle struct {
	ctx    context.Context
	driver *Driver
}

func (h *MongoHandle) Collection(name string) *Collection {
	return &Collection{ctx: h.ctx, driver: h.driver, database: h.driver.database, name: name}
}

func (h *MongoHandle) Db(name string) *MongoDatabaseHandle {
	return &MongoDatabaseHandle{ctx: h.ctx, driver: h.driver, database: h.driver.client.Database(name)}
}

func (h *MongoHandle) Sibling(alias string) *MongoDatabaseHandle {
	name, err := h.driver.resolveSiblingDatabaseName(alias)
	if err != nil {
		panic(err)
	}
	return h.Db(name)
}
