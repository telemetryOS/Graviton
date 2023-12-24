package migrations

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/telemetrytv/graviton-cli/internal/config"
	"go.kuoruan.net/v8go-polyfills/console"
	"go.kuoruan.net/v8go-polyfills/timers"
	"rogchap.com/v8go"
)

const CACHE_PATH = ".graviton/cache"

type Script struct {
	config *config.Config
	ctx    context.Context
	handle any
	src    string
	origin string
}

func NewScript(ctx context.Context, conf *config.Config, handle any, src, origin string) *Script {
	return &Script{
		ctx:    ctx,
		handle: handle,
		src:    src,
		origin: origin,
	}
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

func BuildScriptFromFile(ctx context.Context, conf *config.Config, handle any, origin, path string) (*Script, error) {
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

	return &Script{
		ctx:    ctx,
		handle: handle,
		config: conf,
		src:    string(result.OutputFiles[0].Contents),
		origin: origin,
	}, nil
}

func (s *Script) Name() (string, error) {
	nameVal, err := s.execute("migration.name")
	if err != nil {
		return "", err
	}
	return nameVal.String(), nil
}

func (s *Script) Up() error {
	_, err := s.execute("migration.up(__g__)")
	return err
}

func (s *Script) Down() error {
	_, err := s.execute("migration.down(__g__)")
	return err
}

func (s *Script) execute(scriptSrcSuffix string) (*v8go.Value, error) {
	src := s.src + "\n" + scriptSrcSuffix

	v8Iso := v8go.NewIsolate()
	v8IsoGlobal := v8go.NewObjectTemplate(v8Iso)

	if err := timers.InjectTo(v8Iso, v8IsoGlobal); err != nil {
		return nil, err
	}

	v8Ctx := v8go.NewContext(v8Iso)
	if err := console.InjectTo(v8Ctx); err != nil {
		return nil, err
	}

	v8Ctx.Global().Set("__g__", IntoV8(v8Ctx, s.handle))

	result, err := v8Ctx.RunScript(src, s.origin)
	return result, err
}
