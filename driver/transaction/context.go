package transaction

import "context"

type key struct{}

// Scope binds a database handle and its collections to a transaction lifetime.
type Scope struct{ Active bool }

func New(ctx context.Context) (context.Context, *Scope) {
	scope := &Scope{Active: true}
	return context.WithValue(ctx, key{}, scope), scope
}

func Bound(ctx context.Context) bool {
	scope, ok := ctx.Value(key{}).(*Scope)
	if ok && !scope.Active {
		panic("transaction handle is no longer active")
	}
	return ok
}
