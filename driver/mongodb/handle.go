package mongodb

import (
	"context"
)

type MongoHandle struct {
	ctx    context.Context
	driver *Driver
}

func (h *MongoHandle) Collection(name string) *Collection {
	return &Collection{ctx: h.ctx, driver: h.driver, name: name}
}
