package js

import "github.com/dop251/goja"

func IsObjectFromConstructorWithGlobalName(jsvm *goja.Runtime, val goja.Value, name string) bool {
	ObjCtorVal := GetObjectConstructor(jsvm, val)
	if ObjCtorVal == nil {
		return false
	}
	targetCtorVal := jsvm.GlobalObject().Get(name)
	if targetCtorVal == nil {
		return false
	}
	return ObjCtorVal.StrictEquals(targetCtorVal)
}
