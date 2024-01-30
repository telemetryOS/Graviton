package js

import "github.com/dop251/goja"

func GetObjectConstructor(jsvm *goja.Runtime, val goja.Value) goja.Value {
	obj := val.ToObject(jsvm)
	if obj == nil {
		return nil
	}
	protoObjVal := obj.Prototype()
	if protoObjVal == nil {
		return nil
	}
	protoObj := protoObjVal.ToObject(jsvm)
	if protoObj == nil {
		return nil
	}
	protoObjCtorVal := protoObj.Get("constructor")
	if protoObjCtorVal == nil {
		return nil
	}
	return protoObjCtorVal
}
