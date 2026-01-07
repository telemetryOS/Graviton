package postgresql

import (
	"fmt"
	"strings"

	"github.com/dop251/goja"
	"github.com/xwb1989/sqlparser"
)

type SQLQuery struct {
	Query     string
	Params    []any
	Validated bool
}

func SQLQueryCtor(call goja.ConstructorCall, vm *goja.Runtime) *goja.Object {
	if len(call.Arguments) < 2 {
		panic(vm.ToValue("SQLQuery constructor requires query and params"))
	}

	query := call.Arguments[0].String()

	params, ok := call.Arguments[1].Export().([]any)
	if !ok {
		panic(vm.ToValue("SQLQuery constructor: params must be an array"))
	}

	call.This.Set("query", query)
	call.This.Set("params", params)
	call.This.Set("validated", true)

	return nil
}

func IsSQLQuery(vm *goja.Runtime, val goja.Value, sqlQueryCtorVal goja.Value) bool {
	sqlQueryCtorObj := sqlQueryCtorVal.ToObject(vm)
	if sqlQueryCtorObj == nil {
		return false
	}
	return vm.InstanceOf(val, sqlQueryCtorObj)
}

func SQLQueryFromJSValue(vm *goja.Runtime, val goja.Value) *SQLQuery {
	obj := val.ToObject(vm)
	if obj == nil {
		panic("value is not an object")
	}

	query, ok := obj.Get("query").Export().(string)
	if !ok {
		panic("SQLQuery missing query field")
	}

	var params []any
	if p := obj.Get("params"); p != nil {
		if paramsSlice, ok := p.Export().([]any); ok {
			params = paramsSlice
		}
	}

	validated, _ := obj.Get("validated").Export().(bool)

	return &SQLQuery{
		Query:     query,
		Params:    params,
		Validated: validated,
	}
}

func createSQLTagFunction(d *Driver) func(goja.FunctionCall, *goja.Runtime) goja.Value {
	return func(call goja.FunctionCall, vm *goja.Runtime) goja.Value {
		if len(call.Arguments) == 0 {
			panic(vm.ToValue("sql tag requires at least the template strings"))
		}

		firstArg := call.Arguments[0].Export()

		var parts []string
		if partsSlice, ok := firstArg.([]any); ok {
			for _, part := range partsSlice {
				if str, ok := part.(string); ok {
					parts = append(parts, str)
				}
			}
		} else {
			panic(vm.ToValue("sql tag: first argument must be template strings array"))
		}

		var values []any
		for i := 1; i < len(call.Arguments); i++ {
			values = append(values, call.Arguments[i].Export())
		}

		query := buildPostgreSQLQuery(parts, len(values))

		validateSQL(query)

		sqlQueryInstance, err := vm.New(d.runtimeData[vm].sqlQueryCtorVal, vm.ToValue(query), vm.ToValue(values))
		if err != nil {
			panic(vm.ToValue(fmt.Sprintf("Failed to create SQLQuery instance: %v", err)))
		}

		return sqlQueryInstance
	}
}

func buildPostgreSQLQuery(parts []string, paramCount int) string {
	var query strings.Builder
	paramIndex := 1

	for i, part := range parts {
		query.WriteString(part)
		if i < paramCount {
			query.WriteString(fmt.Sprintf("$%d", paramIndex))
			paramIndex++
		}
	}

	return query.String()
}

func validateSQL(query string) {
	_, err := sqlparser.Parse(query)
	if err != nil {
		fmt.Printf("SQL validation warning (continuing anyway): %v\n", err)
	}
}
