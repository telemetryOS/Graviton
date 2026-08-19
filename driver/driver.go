package driver

import (
	"context"
	"fmt"
	"os"

	"github.com/telemetryos/graviton/config"
	"github.com/telemetryos/graviton/driver/fs"
	"github.com/telemetryos/graviton/driver/mongodb"
	"github.com/telemetryos/graviton/driver/mysql"
	"github.com/telemetryos/graviton/driver/postgresql"
	"github.com/telemetryos/graviton/driver/redis"
	"github.com/telemetryos/graviton/driver/s3"
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

	// AcquireMigrationsLock atomically claims the whole-run migrations lock in
	// this driver's tracking storage (a sibling of the applied-migrations
	// list). When the lock is already taken it returns the current holder and
	// a nil error; the caller formats the user-facing message. Lock operations
	// are immediate — they never join a migration transaction.
	AcquireMigrationsLock(ctx context.Context, lock *migrationsmeta.MigrationsLock) (held *migrationsmeta.MigrationsLock, err error)
	// ReleaseMigrationsLock releases the lock if (and only if) holder still
	// owns it, so a run cannot release a lock that was force-cleared and
	// re-acquired by another run.
	ReleaseMigrationsLock(ctx context.Context, holder string) error
	// GetMigrationsLock returns the current lock, or nil when none is held.
	GetMigrationsLock(ctx context.Context) (*migrationsmeta.MigrationsLock, error)
	// ClearMigrationsLock unconditionally removes the lock. It backs
	// `graviton unlock`, the escape hatch for locks left by crashed runs.
	ClearMigrationsLock(ctx context.Context) error

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

// TransactionDisabler is implemented by drivers that normally wrap a
// migration's writes in a transaction and are able to run without one.
//
// It exists for datasets whose writes cannot fit in a single transaction: a
// MongoDB transaction is bounded by transactionLifetimeLimitSeconds and by a
// hard 16MB total oplog size, and a large ETL exceeds both. Running such a
// migration without a transaction is the operator's explicit choice, made per
// run with `up --no-transactions`, never a default or a silent fallback.
//
// The trade is atomicity: a migration that fails partway leaves the writes it
// already made in place. That is only safe for idempotent, convergent
// migrations, which re-runs bring to the same result. Graviton's recovery model
// already leans on that property — the applied marker is written last, so an
// interrupted migration stays unmarked and re-runs.
//
// Drivers with no transactions to disable (fs, s3, redis) simply do not
// implement it.
type TransactionDisabler interface {
	DisableTransactions()
}

// ValidateDatabaseConfig statically checks one [[databases]] entry beyond the
// structural checks config.Validate performs — currently the per-kind
// connection URL shape for the kinds whose URLs parse without a connection.
// Commands run it before connecting so config mistakes fail fast with a
// pointed message instead of a connection error.
func ValidateDatabaseConfig(conf *config.DatabaseConfig) error {
	var err error
	switch conf.Kind {
	case config.DatabaseKindS3:
		err = s3.ValidateConfig(conf)
	case config.DatabaseKindRedis:
		err = redis.ValidateConfig(conf)
	}
	if err != nil {
		return fmt.Errorf("database %q: %w", conf.Name, err)
	}
	return nil
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
	case config.DatabaseKindFS:
		return fs.New(conf)
	case config.DatabaseKindS3:
		return s3.New(conf)
	case config.DatabaseKindRedis:
		return redis.New(conf)
	default:
		fmt.Println("Unknown database kind: " + string(conf.Kind))
		os.Exit(1)
		return nil
	}
}
