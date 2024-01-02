package mongodb

import (
	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func ObjectId(call goja.ConstructorCall, jsvm *goja.Runtime) *goja.Object {
	var objectId primitive.ObjectID

	if len(call.Arguments) > 0 {
		hexStr := call.Arguments[0].String()
		var err error
		objectId, err = primitive.ObjectIDFromHex(hexStr)
		if err != nil {
			panic(err)
		}
	} else {
		objectId = primitive.NewObjectID()
	}

	call.This.Set("toString", func(call goja.FunctionCall) goja.Value {
		return jsvm.ToValue(objectId.String())
	})

	call.This.Set("toHexString", func(call goja.FunctionCall) goja.Value {
		return jsvm.ToValue(objectId.Hex())
	})

	return nil
}
