package mongodb

import "context"

type Handle struct {
	ctx    context.Context
	driver *Driver
}

func (h *Handle) Collection(name string) *Collection {
	return &Collection{ctx: h.ctx, driver: h.driver, name: name}
}
