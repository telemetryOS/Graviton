package migrations

// TODO: Add flag to allow rolling back a migration from disk

import (
	"context"
	_ "embed"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"github.com/dop251/goja"
	"github.com/evanw/esbuild/pkg/api"
)

const CACHE_PATH = ".graviton/cache"

type Script struct {
	ctx     context.Context
	handle  any
	src     string
	origin  string
	runtime *goja.Runtime
}

func NewScript(ctx context.Context, handle any, src, origin string) *Script {
	script := &Script{
		ctx:    ctx,
		handle: handle,
		src:    src,
		origin: origin,
	}

	script.Evaluate()

	return script
}

type BuildScriptMessage = api.Message

type BuildScriptError struct {
	Errors []BuildScriptMessage
}

func (s *BuildScriptError) Error() string {
	return "failed to compile script"
}

func (s *BuildScriptError) Print() {
	for _, err := range s.Errors {
		fmt.Printf("%s:%d:%d - error:\n%s\n", err.Location.File, err.Location.Line, err.Location.Column, err.Text)
	}
}

func CompileScriptFromFile(ctx context.Context, handle any, origin, path string) (*Script, error) {
	result := api.Build(api.BuildOptions{
		EntryPoints: []string{path},
		Bundle:      true,
		Write:       false,
		Format:      api.FormatIIFE,
		GlobalName:  "migration",
		Target:      api.ES2022,
	})

	if len(result.Errors) != 0 {
		return nil, &BuildScriptError{Errors: result.Errors}
	}

	script := &Script{
		ctx:    ctx,
		handle: handle,
		src:    string(result.OutputFiles[0].Contents),
		origin: origin,
	}
	script.Evaluate()

	return script, nil
}

func (s *Script) Up() error {
	_, err := s.runtime.RunString("migration.up(__g__)")
	return err
}

func (s *Script) Down() error {
	_, err := s.runtime.RunString("migration.down(__g__)")
	return err
}

func (s *Script) Evaluate() {
	s.runtime = goja.New()
	s.runtime.Set("console", JSConsole(s.runtime))
	s.runtime.Set("__g__", IntoJS(s.runtime, s.handle))
	s.runtime.RunScript(s.origin, s.src)
}

func JSConsole(jsvm *goja.Runtime) *goja.Object {
	console := jsvm.NewObject()
	console.Set("log", func(call goja.FunctionCall) goja.Value {
		fmt.Println(call.Arguments)
		return goja.Undefined()
	})
	return console
}

// IntoJS converts a go value into a goja value. Note that goja has it's own
// method for doing this, but it doesn't copy the value, nor does it rename
// properties to follow JS conventions.
func IntoJS(jsvm *goja.Runtime, v any) goja.Value {
	return intoJs(jsvm, reflect.ValueOf(v))
}

func intoJs(jsvm *goja.Runtime, vr reflect.Value) goja.Value {
	switch vr.Kind() {
	case reflect.Bool,
		reflect.String,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Float32,
		reflect.Float64:
		return jsvm.ToValue(vr.Interface())
	case reflect.Slice:
		arr := jsvm.NewArray()
		for i := 0; i < vr.Len(); i += 1 {
			arr.Set(strconv.Itoa(i), intoJs(jsvm, vr.Index(i)))
		}
		return arr
	case reflect.Map:
		obj := jsvm.NewObject()
		for _, key := range vr.MapKeys() {
			obj.Set(key.String(), intoJs(jsvm, vr.MapIndex(key)))
		}
		return obj
	case reflect.Struct:
		obj := jsvm.NewObject()
		for i := 0; i < vr.NumField(); i += 1 {
			field := vr.Type().Field(i)
			if unicode.IsUpper(rune(field.Name[0])) {
				jsName := strings.ToLower(field.Name[0:1]) + field.Name[1:]
				obj.Set(jsName, intoJs(jsvm, vr.Field(i)))
			}
		}
		for i := 0; i < vr.NumMethod(); i += 1 {
			method := vr.Type().Method(i)
			if unicode.IsUpper(rune(method.Name[0])) {
				jsName := strings.ToLower(method.Name[0:1]) + method.Name[1:]
				obj.Set(jsName, intoJs(jsvm, vr.Method(i)))
			}
		}
		if vr.CanAddr() {
			vrAddr := vr.Addr()
			for i := 0; i < vrAddr.NumMethod(); i += 1 {
				method := vrAddr.Type().Method(i)
				if unicode.IsUpper(rune(method.Name[0])) {
					jsName := strings.ToLower(method.Name[0:1]) + method.Name[1:]
					obj.Set(jsName, intoJs(jsvm, vrAddr.Method(i)))
				}
			}
		}
		return obj
	case reflect.Func:
		return jsvm.ToValue(func(call goja.FunctionCall) goja.Value {
			argsVrs := []reflect.Value{}
			for _, arg := range call.Arguments {
				argsVrs = append(argsVrs, reflect.ValueOf(arg.Export()))
			}
			rtnVrs := vr.Call(argsVrs)
			switch len(rtnVrs) {
			case 0:
				return goja.Undefined()
			case 1:
				return intoJs(jsvm, rtnVrs[0])
			default:
				arr := jsvm.NewArray()
				for i := 0; i < len(rtnVrs); i += 1 {
					arr.Set(strconv.Itoa(i), intoJs(jsvm, rtnVrs[i]))
				}
				return arr
			}
		})
	case reflect.Ptr, reflect.Interface:
		return intoJs(jsvm, vr.Elem())
	case reflect.Array:
		arr := jsvm.NewArray()
		for i := 0; i < vr.Len(); i += 1 {
			arr.Set(strconv.Itoa(i), intoJs(jsvm, vr.Index(i)))
		}
		return arr
	default:
		println("calling unfinished go type", vr.Kind().String())
		return goja.Undefined()
	}
}
