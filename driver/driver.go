package driver

import (
	"context"
	"fmt"
	"os"

	"github.com/telemetryos/graviton/config"
	"github.com/telemetryos/graviton/driver/mongodb"
	"github.com/telemetryos/graviton/driver/mysql"
	"github.com/telemetryos/graviton/driver/postgresql"
	"github.com/telemetryos/graviton/driver/sqlite"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"github.com/dop251/goja"
)

// Driver is one connected database in a run. Each driver owns a single session
// (for kinds that have one) per run and, at most, one open transaction at a
// time. Transactions are managed explicitly by the runner so that a single
// migration can interleave writes across several drivers and then commit them
// as independent, per-handle transactions.
type Driver interface {
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	GetAppliedMigrationsMetadata(ctx context.Context) ([]*migrationsmeta.MigrationMetadata, error)
	SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []*migrationsmeta.MigrationMetadata) error

	// BeginTx opens a transaction if one is not already open. It is idempotent:
	// the JS-facing handles lazily begin a transaction on their first operation,
	// and the runner may also begin one explicitly (e.g. to write the applied
	// marker). Calling BeginTx while a transaction is already open is a no-op.
	BeginTx(ctx context.Context) error
	// CommitTx commits the open transaction, if any, and leaves the driver ready
	// to begin another. It is a no-op when no transaction is open.
	CommitTx(ctx context.Context) error
	// RollbackTx aborts the open transaction, if any. It is a no-op when no
	// transaction is open and never returns an error the caller must act on.
	RollbackTx(ctx context.Context) error
	// HasOpenTx reports whether a transaction is currently open on this driver.
	HasOpenTx() bool

	Handle(ctx context.Context) any
	Init(ctx context.Context, runtime *goja.Runtime)
	Globals(ctx context.Context, runtime *goja.Runtime) map[string]any
	MaybeFromJSValue(ctx context.Context, runtime *goja.Runtime, value goja.Value) (any, bool)
	// MaybeIntoJSValue is the Go→JS counterpart of MaybeFromJSValue: it lets a
	// driver surface its native types (e.g. MongoDB ObjectIDs) to migration
	// scripts as their proper JS representation instead of the generic
	// reflection-based conversion.
	MaybeIntoJSValue(ctx context.Context, runtime *goja.Runtime, value any) (goja.Value, bool)
}

// FromDatabaseConfig builds the driver for conf.
func FromDatabaseConfig(conf *config.DatabaseConfig) Driver {
	if conf == nil {
		fmt.Println("Unknown database")
		os.Exit(1)
		return nil
	}
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
