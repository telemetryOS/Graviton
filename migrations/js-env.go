package migrations

import (
	"fmt"
	"os"

	"github.com/dop251/goja"
)

// JSEnv builds the `env` global: read access to the process environment.
//
//	env.get("EDM_AES_KEY_V1") -> string
//
// Migrations otherwise have no way to vary by environment — a batch size, a
// target host, or the key material a legacy dataset was encrypted with. Config
// substitution (${VAR} in graviton.config.toml) covers connections only.
//
// A missing variable throws naming it rather than returning "". An empty value
// silently flowing into a decrypt would produce authentication failures far
// from the cause, and into a comparison would quietly change what a migration
// matches.
func JSEnv(jsvm *goja.Runtime) *goja.Object {
	env := jsvm.NewObject()

	env.Set("get", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		value, ok := os.LookupEnv(name)
		if !ok {
			panic(jsvm.NewGoError(fmt.Errorf(
				"environment variable %q is not set; migrations that need it should be run with it in the environment", name)))
		}
		return jsvm.ToValue(value)
	})

	env.Set("has", func(call goja.FunctionCall) goja.Value {
		_, ok := os.LookupEnv(call.Argument(0).String())
		return jsvm.ToValue(ok)
	})

	return env
}
