package mongodb

import (
	"graviton/internal/migrations/js"

	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func JSObjectIdCtor(call goja.ConstructorCall, jsvm *goja.Runtime) *goja.Object {
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

func IsObjectId(jsvm *goja.Runtime, val goja.Value) bool {
	return js.IsObjectFromConstructorWithGlobalName(jsvm, val, "ObjectId")
}

func ObjectIdFromJSValue(jsvm *goja.Runtime, val goja.Value) primitive.ObjectID {
	toHexStringVal := val.ToObject(jsvm).Get("toHexString")
	toHexString, ok := goja.AssertFunction(toHexStringVal)
	if !ok {
		panic("ObjectId.toHexString is not a function")
	}
	returnVal, err := toHexString(goja.Undefined(), nil)
	if err != nil {
		panic(err)
	}
	goObjectId, err := primitive.ObjectIDFromHex(returnVal.String())
	if err != nil {
		panic(err)
	}
	return goObjectId
}
