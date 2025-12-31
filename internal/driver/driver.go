package driver

import (
	"context"
	"fmt"
	"os"

	"graviton/internal/config"
	"graviton/internal/driver/mongodb"
	migrationsmeta "graviton/internal/migrations-meta"

	"github.com/dop251/goja"
)

type Driver interface {
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	GetAppliedMigrationsMetadata(ctx context.Context) ([]*migrationsmeta.MigrationMetadata, error)
	SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []*migrationsmeta.MigrationMetadata) error
	WithTransaction(ctx context.Context, fn func() error) error
	Handle(ctx context.Context) any
	Globals(ctx context.Context) map[string]any
	MaybeFromJSValue(ctx context.Context, runtime *goja.Runtime, value goja.Value) (any, bool)
}

func FromDatabaseConfig(conf *config.DatabaseConfig) Driver {
	switch conf.Kind {
	case config.DatabaseKindMongoDB:
		return mongodb.New(conf)
	default:
		fmt.Println("Unknown database kind: " + string(conf.Kind))
		os.Exit(1)
		return nil
	}
}
