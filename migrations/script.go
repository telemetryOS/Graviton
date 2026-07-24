package migrations

// TODO: Add flag to allow rolling back a migration from disk

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	_ "embed"

	"github.com/telemetryos/graviton/driver"
	"github.com/telemetryos/graviton/migrations/js"

	"github.com/dop251/goja"
	"github.com/evanw/esbuild/pkg/api"
)

const CACHE_PATH = ".graviton/cache"

var dummyJsFn = func(call goja.FunctionCall) goja.Value { return goja.Undefined() }
var dummyJsCtor = func(call goja.ConstructorCall) *goja.Object { return nil }
var dummyJsFnWithRuntime = func(call goja.FunctionCall, jsvm *goja.Runtime) goja.Value { return goja.Undefined() }
var dummyJsCtorWithRuntime = func(call goja.ConstructorCall, jsvm *goja.Runtime) *goja.Object { return nil }

// Script is a single compiled migration bound to the whole run. handle is the
// root JS handle (__g__): a unified handle carrying an optional bound database.
// In a single-database project it is already bound to the only database; in a
// multi-database project it is unbound and its use(alias) method returns a fresh
// bound handle. drivers is every driver participating in the run, so the JS
// value hooks (MaybeIntoJSValue/MaybeFromJSValue) and Globals of every active
// driver are consulted while a migration interleaves work across them.
type Script struct {
	ctx     context.Context
	drivers []driver.Driver
	handle  any
	src     string
	origin  string
	runtime *goja.Runtime
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

// buildMigrationSource bundles a migration file into a single IIFE module. The
// resulting source assigns the module to the `migration` global.
func buildMigrationSource(path string) (string, *BuildScriptError) {
	result := api.Build(api.BuildOptions{
		EntryPoints: []string{path},
		Bundle:      true,
		Write:       false,
		Format:      api.FormatIIFE,
		GlobalName:  "migration",
		Target:      api.ES2022,
	})

	if len(result.Errors) != 0 {
		return "", &BuildScriptError{Errors: result.Errors}
	}

	return string(result.OutputFiles[0].Contents), nil
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

	// Register every participating driver's runtime data before gathering their
	// globals; Globals reads the per-runtime state Init installs.
	for _, d := range s.drivers {
		d.Init(s.ctx, s.runtime)
	}

	// The root handle exposes use()/collection()/the SQL surface as methods, so
	// reflection surfaces them directly onto __g__ — no separate injection.
	s.runtime.Set("__g__", s.intoJs(reflect.ValueOf(s.handle)))

	// Globals from every driver merge onto the runtime (e.g. ObjectId from a
	// mongodb driver, sql/SQLQuery from a SQL driver).
	for _, d := range s.drivers {
		for name, value := range d.Globals(s.ctx, s.runtime) {
			s.runtime.Set(name, s.intoJs(reflect.ValueOf(value)))
		}
	}

	s.runtime.RunScript(s.origin, s.src)
}

func (s *Script) intoJs(vr reflect.Value) goja.Value {
	// A zero Value comes from dereferencing a nil pointer/interface (e.g. the
	// nil UpsertedID on a mongo.UpdateResult when no upsert occurred). Calling
	// Interface on it panics, so surface it to JS as null instead.
	if !vr.IsValid() {
		return goja.Null()
	}

	intf := vr.Interface()

	if gojaVal, ok := intf.(goja.Value); ok {
		return gojaVal
	}

	// Driver-native types first (e.g. MongoDB ObjectIDs become ObjectId class
	// instances rather than falling into the generic array conversion). With
	// several active drivers, the first driver whose hook claims the value wins.
	if s.runtime != nil {
		for _, d := range s.drivers {
			if driverVal, ok := d.MaybeIntoJSValue(s.ctx, s.runtime, intf); ok {
				return driverVal
			}
		}
	}

	if gojaObj, ok := intf.(*goja.Object); ok {
		return gojaObj
	}

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
		return s.runtime.ToValue(vr.Interface())
	case reflect.Slice:
		arr := s.runtime.NewArray()
		for i := 0; i < vr.Len(); i += 1 {
			arr.Set(strconv.Itoa(i), s.intoJs(vr.Index(i)))
		}
		return arr
	case reflect.Map:
		// A nil map (e.g. FindOne's no-match result) is null in JS, not a
		// truthy empty object — find-before-insert patterns depend on this.
		if vr.IsNil() {
			return goja.Null()
		}
		obj := s.runtime.NewObject()
		for _, key := range vr.MapKeys() {
			obj.Set(key.String(), s.intoJs(vr.MapIndex(key)))
		}
		return obj
	case reflect.Struct:
		obj := s.runtime.NewObject()
		for i := 0; i < vr.NumField(); i += 1 {
			field := vr.Type().Field(i)
			if unicode.IsUpper(rune(field.Name[0])) {
				jsName := strings.ToLower(field.Name[0:1]) + field.Name[1:]
				obj.Set(jsName, s.intoJs(vr.Field(i)))
			}
		}
		for i := 0; i < vr.NumMethod(); i += 1 {
			method := vr.Type().Method(i)
			if unicode.IsUpper(rune(method.Name[0])) {
				jsName := strings.ToLower(method.Name[0:1]) + method.Name[1:]
				obj.Set(jsName, s.intoJs(vr.Method(i)))
			}
		}
		if vr.CanAddr() {
			vrAddr := vr.Addr()
			for i := 0; i < vrAddr.NumMethod(); i += 1 {
				method := vrAddr.Type().Method(i)
				if unicode.IsUpper(rune(method.Name[0])) {
					jsName := strings.ToLower(method.Name[0:1]) + method.Name[1:]
					obj.Set(jsName, s.intoJs(vrAddr.Method(i)))
				}
			}
		}
		return obj
	case reflect.Func:
		tr := vr.Type()

		switch {
		case tr.ConvertibleTo(reflect.TypeOf(dummyJsCtor)),
			tr.ConvertibleTo(reflect.TypeOf(dummyJsFn)),
			tr.ConvertibleTo(reflect.TypeOf(dummyJsCtorWithRuntime)),
			tr.ConvertibleTo(reflect.TypeOf(dummyJsFnWithRuntime)):
			return s.runtime.ToValue(vr.Interface())

		default:
			return s.runtime.ToValue(func(call goja.FunctionCall) goja.Value {
				argsVrs := []reflect.Value{}
				for _, arg := range call.Arguments {
					argsVrs = append(argsVrs, reflect.ValueOf(s.fromJs(arg)))
				}
				rtnVrs := vr.Call(argsVrs)
				switch len(rtnVrs) {
				case 0:
					return goja.Undefined()
				case 1:
					return s.intoJs(rtnVrs[0])
				default:
					arr := s.runtime.NewArray()
					for i := 0; i < len(rtnVrs); i += 1 {
						arr.Set(strconv.Itoa(i), s.intoJs(rtnVrs[i]))
					}
					return arr
				}
			})
		}
	case reflect.Ptr, reflect.Interface:
		if vr.IsNil() {
			return goja.Null()
		}
		return s.intoJs(vr.Elem())
	case reflect.Array:
		arr := s.runtime.NewArray()
		for i := 0; i < vr.Len(); i += 1 {
			arr.Set(strconv.Itoa(i), s.intoJs(vr.Index(i)))
		}
		return arr
	default:
		println("calling unfinished go type", vr.Kind().String())
		return goja.Undefined()
	}
}

func (s *Script) fromJs(val goja.Value) any {
	// Driver-native values first: a host-wrapped driver type (ObjectId
	// instance, BSON binary, SQLQuery, …) would otherwise be decomposed by the
	// generic Object branch below, dragging its methods along as function
	// values. Every participating driver is consulted so a value minted by one
	// driver's global still round-trips when handed to another driver's handle.
	for _, d := range s.drivers {
		if goVal, ok := d.MaybeFromJSValue(s.ctx, s.runtime, val); ok {
			return goVal
		}
	}
	switch {
	case js.IsObjectFromConstructorWithGlobalName(s.runtime, val, "Array"):
		arr := val.ToObject(s.runtime)
		arrLen := int(arr.Get("length").ToInteger())
		goVal := []any{}
		for i := 0; i < arrLen; i += 1 {
			goVal = append(goVal, s.fromJs(arr.Get(strconv.Itoa(i))))
		}
		return goVal
	case js.IsObjectFromConstructorWithGlobalName(s.runtime, val, "Object"):
		obj := val.ToObject(s.runtime)
		goVal := map[string]any{}
		for _, key := range obj.Keys() {
			goVal[key] = s.fromJs(obj.Get(key))
		}
		return goVal
	default:
		return val.Export()
	}
}

func JSConsole(jsvm *goja.Runtime) *goja.Object {
	console := jsvm.NewObject()
	console.Set("log", func(call goja.FunctionCall) goja.Value {
		fmt.Println(call.Arguments)
		return goja.Undefined()
	})
	return console
}
