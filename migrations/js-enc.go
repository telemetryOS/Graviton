package migrations

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/dop251/goja"
)

// encodings lists what decodeString and encodeBytes accept. It appears in the
// error message for an unknown name, so a typo tells the author what is valid.
const encodings = "base64, base64url, hex, utf8"

// JSEnc builds the `enc` global: conversions between encoded strings and raw
// bytes.
//
//	enc.decode("base64", str) -> bytes
//	enc.encode("utf8", bytes) -> string
//
// It is deliberately separate from `crypto`. Migrations meet base64 blobs, hex
// identifiers and byte/string conversion in plenty of places that have nothing
// to do with keys, and keeping conversion here means crypto only ever sees
// bytes — so a call reads as exactly what it does rather than relying on an
// options bag to say which end was encoded.
//
// Encodings are named explicitly and an unknown name throws. Guessing would
// turn a typo into silently wrong data, which a migration writes to the target
// database before anyone notices.
func JSEnc(jsvm *goja.Runtime) *goja.Object {
	enc := jsvm.NewObject()

	enc.Set("decode", func(call goja.FunctionCall) goja.Value {
		encoding := call.Argument(0).String()
		text := call.Argument(1).String()
		raw, err := decodeString(encoding, text)
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		return jsvm.ToValue(jsvm.NewArrayBuffer(raw))
	})

	enc.Set("encode", func(call goja.FunctionCall) goja.Value {
		encoding := call.Argument(0).String()
		raw, err := BytesFromJS(jsvm, call.Argument(1))
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		text, err := encodeBytes(encoding, raw)
		if err != nil {
			panic(jsvm.NewGoError(err))
		}
		return jsvm.ToValue(text)
	})

	return enc
}

func decodeString(encoding string, text string) ([]byte, error) {
	switch encoding {
	case "base64":
		return base64.StdEncoding.DecodeString(text)
	case "base64url":
		return base64.URLEncoding.DecodeString(text)
	case "hex":
		return hex.DecodeString(text)
	case "utf8":
		return []byte(text), nil
	default:
		return nil, fmt.Errorf("unknown encoding %q; supported: %s", encoding, encodings)
	}
}

func encodeBytes(encoding string, raw []byte) (string, error) {
	switch encoding {
	case "base64":
		return base64.StdEncoding.EncodeToString(raw), nil
	case "base64url":
		return base64.URLEncoding.EncodeToString(raw), nil
	case "hex":
		return hex.EncodeToString(raw), nil
	case "utf8":
		return string(raw), nil
	default:
		return "", fmt.Errorf("unknown encoding %q; supported: %s", encoding, encodings)
	}
}

// BytesFromJS accepts the shapes a migration can plausibly hold bytes in: an
// ArrayBuffer (what enc.decode and the crypto functions return), a byte array,
// or a string, which is taken as UTF-8. Anything else is a mistake worth naming
// rather than coercing.
func BytesFromJS(jsvm *goja.Runtime, val goja.Value) ([]byte, error) {
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return nil, fmt.Errorf("expected bytes, got %v", val)
	}
	switch exported := val.Export().(type) {
	case goja.ArrayBuffer:
		return exported.Bytes(), nil
	case []byte:
		return exported, nil
	case string:
		return []byte(exported), nil
	case []any:
		raw := make([]byte, len(exported))
		for i, item := range exported {
			num, ok := toByte(item)
			if !ok {
				return nil, fmt.Errorf("expected a byte array; element %d is %v", i, item)
			}
			raw[i] = num
		}
		return raw, nil
	default:
		return nil, fmt.Errorf("expected bytes (ArrayBuffer, byte array or string), got %T", exported)
	}
}

func toByte(item any) (byte, bool) {
	var n int64
	switch v := item.(type) {
	case int64:
		n = v
	case float64:
		n = int64(v)
		if float64(n) != v {
			return 0, false
		}
	default:
		return 0, false
	}
	if n < 0 || n > 255 {
		return 0, false
	}
	return byte(n), true
}
