package postgresql

import (
	"context"

	"github.com/dop251/goja"
)

// MaybeIntoJSValue: no driver-native Go→JS conversions for postgresql; the generic
// reflection conversion handles everything.
func (d *Driver) MaybeIntoJSValue(ctx context.Context, jsvm *goja.Runtime, value any) (goja.Value, bool) {
	return nil, false
}
