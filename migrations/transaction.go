package migrations

import (
	"fmt"
	"reflect"

	"github.com/dop251/goja"
	"github.com/telemetryos/graviton/driver/transaction"
)

func (s *Script) withTransaction(h *Handle) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		defer func() {
			if recovered := recover(); recovered != nil {
				if value, ok := recovered.(goja.Value); ok {
					panic(value)
				}
				panic(s.runtime.NewGoError(fmt.Errorf("%v", recovered)))
			}
		}()
		callback, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			panic(s.runtime.NewTypeError("withTransaction requires an async callback"))
		}
		if h.drv == nil {
			panic(s.runtime.NewTypeError("select a database with use() first"))
		}
		transaction.Bound(h.ctx)
		if h.drv.HasOpenTx() {
			panic(s.runtime.NewTypeError("database already has an active transaction"))
		}
		if err := h.drv.BeginTx(h.ctx); err != nil {
			panic(s.runtime.NewGoError(err))
		}
		if !h.drv.HasOpenTx() {
			panic(s.runtime.NewTypeError("database does not support transactions"))
		}
		ctx, scope := transaction.New(h.ctx)
		bound := *h
		bound.ctx = ctx
		promise, resolve, reject := s.runtime.NewPromise()
		fail := func(reason goja.Value) {
			scope.Active = false
			if err := h.drv.RollbackTx(h.ctx); err != nil {
				reject(s.runtime.NewGoError(fmt.Errorf("transaction failed (%s); rollback failed: %w", reason, err)))
			} else {
				reject(reason)
			}
		}
		result, err := callback(goja.Undefined(), s.intoJs(reflect.ValueOf(&bound)))
		if err != nil {
			fail(s.runtime.ToValue(err))
			return s.runtime.ToValue(promise)
		}
		if _, ok := result.Export().(*goja.Promise); !ok {
			fail(s.runtime.NewTypeError("withTransaction callback must return a Promise"))
			return s.runtime.ToValue(promise)
		}
		then, _ := goja.AssertFunction(result.ToObject(s.runtime).Get("then"))
		_, err = then(result, s.runtime.ToValue(func(call goja.FunctionCall) goja.Value {
			scope.Active = false
			if err := h.drv.CommitTx(h.ctx); err != nil {
				h.drv.RollbackTx(h.ctx)
				reject(s.runtime.NewGoError(err))
			} else {
				resolve(call.Argument(0))
			}
			return goja.Undefined()
		}), s.runtime.ToValue(func(call goja.FunctionCall) goja.Value {
			fail(call.Argument(0))
			return goja.Undefined()
		}))
		if err != nil {
			fail(s.runtime.ToValue(err))
		}
		return s.runtime.ToValue(promise)
	}
}
