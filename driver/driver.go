package driver

import (
	"context"
	"fmt"
	"os"

	"graviton/config"
	"graviton/driver/mongodb"
	"graviton/driver/mysql"
	"graviton/driver/postgresql"
	"graviton/driver/sqlite"
	migrationsmeta "graviton/migrations-meta"

	"github.com/dop251/goja"
)

type Driver interface {
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	GetAppliedMigrationsMetadata(ctx context.Context) ([]*migrationsmeta.MigrationMetadata, error)
	SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []*migrationsmeta.MigrationMetadata) error
	WithTransaction(ctx context.Context, fn func(context.Context) error) error
	Handle(ctx context.Context) any
	Init(ctx context.Context, runtime *goja.Runtime)
	Globals(ctx context.Context, runtime *goja.Runtime) map[string]any
	MaybeFromJSValue(ctx context.Context, runtime *goja.Runtime, value goja.Value) (any, bool)
}

func FromDatabaseConfig(conf *config.DatabaseConfig) Driver {
	switch conf.Kind {
	case config.DatabaseKindMongoDB:
		return mongodb.New(conf)
	case config.DatabaseKindPostgreSQL:
		return postgresql.New(conf)
	case config.DatabaseKindMySQL:
		return mysql.New(conf)
	case config.DatabaseKindSQLite:
		return sqlite.New(conf)
	default:
		fmt.Println("Unknown database kind: " + string(conf.Kind))
		os.Exit(1)
		return nil
	}
}
