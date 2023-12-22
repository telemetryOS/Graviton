package migrations

import (
	"context"
	_ "embed"

	"github.com/risor-io/risor"
	"github.com/risor-io/risor/object"
)

// TODO: Consider using typescript instead via
// https://pkg.go.dev/github.com/dop251/goja

//go:embed embed/assertions.risor
var assertionsSrc string

//go:embed embed/down.risor
var downSrc string

//go:embed embed/name.risor
var nameSrc string

//go:embed embed/up.risor
var upSrc string

type Script struct {
	ctx    context.Context
	handle any
	src    string
}

func NewScript(ctx context.Context, handle any, src string) *Script {
	return &Script{ctx: ctx, handle: handle, src: src}
}

func (s *Script) Name() (string, error) {
	name, err := s.execute(nameSrc)
	if err != nil {
		return "", err
	}
	return name.Interface().(string), nil
}

func (s *Script) Up() error {
	_, err := s.execute(upSrc)
	return err
}

func (s *Script) Down() error {
	_, err := s.execute(downSrc)
	return err
}

func (s *Script) execute(postScriptSrc string) (object.Object, error) {
	src := s.src + "\n" + assertionsSrc + postScriptSrc
	result, err := risor.Eval(s.ctx, src, risor.WithGlobal("__g__", s.handle))
	if err != nil {
		return nil, err
	}
	return result, nil
}
