package migrations

import (
	"fmt"
	"reflect"
	"strings"

	"rogchap.com/v8go"
)

func IntoV8(v8Ctx *v8go.Context, val any) *v8go.Value {
	return intoV8(v8Ctx, reflect.ValueOf(val))
}

func intoV8(v8Ctx *v8go.Context, v reflect.Value) *v8go.Value {
	switch v.Kind() {
	case reflect.String:
		val, err := v8go.NewValue(v8Ctx.Isolate(), v.String())
		if err != nil {
			panic(err)
		}
		return val

	case reflect.Bool:
		val, err := v8go.NewValue(v8Ctx.Isolate(), v.Bool())
		if err != nil {
			panic(err)
		}
		return val

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val, err := v8go.NewValue(v8Ctx.Isolate(), v.Int())
		if err != nil {
			panic(err)
		}
		return val

	case reflect.Float32, reflect.Float64:
		val, err := v8go.NewValue(v8Ctx.Isolate(), v.Float())
		if err != nil {
			panic(err)
		}
		return val

	case reflect.Slice:
		arrCtorVal, err := v8Ctx.Global().Get("Array")
		if err != nil {
			panic(err)
		}
		arrCtor, err := arrCtorVal.AsFunction()
		if err != nil {
			panic(err)
		}
		arr, err := arrCtor.NewInstance()
		if err != nil {
			panic(err)
		}
		for i := 0; i < v.Len(); i += 1 {
			arr.SetIdx(uint32(i), intoV8(v8Ctx, v.Index(i)))
		}
		return arr.Value

	case reflect.Map:
		objTemplate := v8go.NewObjectTemplate(v8Ctx.Isolate())

		for _, key := range v.MapKeys() {
			objTemplate.Set(key.String(), intoV8(v8Ctx, v.MapIndex(key)))
		}

		obj, err := objTemplate.NewInstance(v8Ctx)
		if err != nil {
			panic(err)
		}
		return obj.Value

	case reflect.Ptr:
		return intoV8(v8Ctx, v.Elem())

	case reflect.Struct:
		objCtorVal, err := v8Ctx.Global().Get("Object")
		if err != nil {
			panic(err)
		}
		objCtor, err := objCtorVal.AsFunction()
		if err != nil {
			panic(err)
		}
		obj, err := objCtor.NewInstance()
		if err != nil {
			panic(err)
		}

		for i := 0; i < v.NumField(); i += 1 {
			v8FieldName := v.Type().Field(i).Tag.Get("v8")
			field := v.Field(i)
			if field.CanInterface() {
				obj.Set(v8FieldName, intoV8(v8Ctx, v.Field(i)))
			}
		}

		for i := 0; i < v.NumMethod(); i += 1 {
			methodName := v.Type().Method(i).Name
			v8MethodName := strings.ToLower(methodName[0:1]) + methodName[1:]
			obj.Set(v8MethodName, intoV8(v8Ctx, v.Method(i)))
		}

		if v.CanAddr() {
			pv := v.Addr()

			for i := 0; i < pv.NumMethod(); i += 1 {
				methodName := pv.Type().Method(i).Name
				v8MethodName := strings.ToLower(methodName[0:1]) + methodName[1:]
				obj.Set(v8MethodName, intoV8(v8Ctx, pv.Method(i)))
			}
		}
		return obj.Value

	case reflect.Func:
		fnTemplate := v8go.NewFunctionTemplate(v8Ctx.Isolate(), func(info *v8go.FunctionCallbackInfo) *v8go.Value {
			t := v.Type()
			args := []reflect.Value{}

			if t.NumIn() > len(info.Args()) {
				panic("not enough arguments")
			}

			for i := 0; i < t.NumIn(); i += 1 {
				argT := t.In(i)
				v8Arg := info.Args()[i]
				args = append(args, FromV8ToReflectValueByType(v8Ctx, v8Arg, argT))
			}

			rtnValue := v.Call(args)

			switch len(rtnValue) {
			case 0:
				return v8go.Undefined(v8Ctx.Isolate())
			case 1:
				return intoV8(v8Ctx, rtnValue[0])
			default:
				rtnSlice := []any{}
				for _, rtnSubVal := range rtnValue {
					rtnSlice = append(rtnSlice, rtnSubVal.Interface())
				}
				return IntoV8(v8Ctx, rtnSlice)
			}
		})

		fn := fnTemplate.GetFunction(v8Ctx)
		return fn.Value

	default:
		panic(fmt.Sprintf("cannot convert %s to v8 value", v.Kind()))
	}
}

func FromV8ToReflectValueByType(v8Ctx *v8go.Context, v8Val *v8go.Value, t reflect.Type) reflect.Value {
	switch t.Kind() {
	case reflect.String:
		return reflect.ValueOf(v8Val.String())
	case reflect.Bool:
		return reflect.ValueOf(v8Val.Boolean())
	case reflect.Int:
		return reflect.ValueOf(int(v8Val.Number()))
	case reflect.Int8:
		return reflect.ValueOf(int8(v8Val.Number()))
	case reflect.Int16:
		return reflect.ValueOf(int16(v8Val.Number()))
	case reflect.Int32:
		return reflect.ValueOf(int32(v8Val.Number()))
	case reflect.Int64:
		return reflect.ValueOf(int64(v8Val.Number()))
	case reflect.Uint:
		return reflect.ValueOf(uint(v8Val.Number()))
	case reflect.Uint8:
		return reflect.ValueOf(uint8(v8Val.Number()))
	case reflect.Uint16:
		return reflect.ValueOf(uint16(v8Val.Number()))
	case reflect.Uint32:
		return reflect.ValueOf(uint32(v8Val.Number()))
	case reflect.Uint64:
		return reflect.ValueOf(uint64(v8Val.Number()))
	case reflect.Float32:
		return reflect.ValueOf(float32(v8Val.Number()))
	case reflect.Float64:
		return reflect.ValueOf(v8Val.Number())
	case reflect.Interface:
		return FromV8ToReflectValue(v8Ctx, v8Val)
	default:
		panic("encountered unsupported conversion to go type" + t.String())
	}
}

func FromV8ToReflectValue(v8Ctx *v8go.Context, v8Val *v8go.Value) reflect.Value {
	switch {
	case v8Val.IsString(), v8Val.IsSymbol():
		return reflect.ValueOf(v8Val.String())
	case v8Val.IsBoolean():
		return reflect.ValueOf(v8Val.Boolean())
	case v8Val.IsNumber():
		return reflect.ValueOf(v8Val.Number())
	// case v8Val.IsObject():
	// 	panic("not yet implemented")
	// case v8Val.IsFunction():
	// 	panic("not yet implemented")
	case v8Val.IsUndefined(), v8Val.IsNull():
		return reflect.ValueOf(nil)
	case v8Val.IsBigInt():
		return reflect.ValueOf(v8Val.BigInt())
	case v8Val.IsDate():
		toIsoStrMthdVal, err := v8Val.Object().Get("toISOString")
		if err != nil {
			panic(err)
		}
		toIsoStrMthd, err := toIsoStrMthdVal.AsFunction()
		if err != nil {
			panic(err)
		}
		isoStrVal, err := toIsoStrMthd.Call(v8Ctx.Global())
		if err != nil {
			panic(err)
		}
		return reflect.ValueOf(isoStrVal.String())
	default:
		panic("encountered unsupported v8 value type" + v8Val.DetailString())
	}
}
