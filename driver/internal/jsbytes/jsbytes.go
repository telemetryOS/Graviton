// Package jsbytes holds the byte-payload conversions shared by the file-like
// drivers (fs, s3). Their handles read and write raw bytes, which migration
// scripts see as ArrayBuffer values; text convenience methods use plain
// strings.
package jsbytes

import (
	"github.com/dop251/goja"
)

// ToBytes converts a JS-supplied body value into raw bytes. Migration scripts
// may pass a string (text content) or an ArrayBuffer (binary content, e.g. the
// result of readBytes()/getBytes() on another handle).
func ToBytes(value any) ([]byte, bool) {
	switch v := value.(type) {
	case string:
		return []byte(v), true
	case []byte:
		return v, true
	case goja.ArrayBuffer:
		return v.Bytes(), true
	}
	return nil, false
}

// MaybeIntoJSValue surfaces []byte returns (readBytes()/getBytes()) to
// migration scripts as ArrayBuffer values instead of the generic
// number-per-element array conversion.
func MaybeIntoJSValue(jsvm *goja.Runtime, value any) (goja.Value, bool) {
	if b, ok := value.([]byte); ok {
		return jsvm.ToValue(jsvm.NewArrayBuffer(b)), true
	}
	return nil, false
}
