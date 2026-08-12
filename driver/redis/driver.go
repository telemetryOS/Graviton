// Package redis is the Redis key-value store driver. It speaks the Redis wire
// protocol, so Valkey (and other protocol-compatible servers) work with it the
// same way MariaDB works with the mysql driver. It exists so cache/KV state
// can take part in the same linear migration set as databases — e.g. renaming
// key namespaces or rewriting stored values when the rows they mirror change.
//
// Redis MULTI/EXEC queues commands without returning read results until EXEC,
// which would break the read-then-write pattern migrations rely on. Handle
// operations therefore apply immediately: BeginTx/CommitTx/RollbackTx are
// no-ops, and a failed migration body does NOT undo writes that already
// happened. This matches Graviton's recovery model — idempotent/convergent
// migrations plus re-run.
package redis

import (
	"context"
	"errors"
	"fmt"

	"github.com/telemetryos/graviton/config"
	"github.com/telemetryos/graviton/driver/internal/jsontracking"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"github.com/dop251/goja"
	goredis "github.com/redis/go-redis/v9"
)

// MIGRATIONS_KEY is the tracking document's key when this driver is the
// migrations_db.
const MIGRATIONS_KEY = "graviton-migrations"

type Driver struct {
	config *config.DatabaseConfig
	client *goredis.Client
}

// New builds a Redis driver for conf. connection_url is a standard
// redis:// / rediss:// URL (redis://user:pass@host:6379/0); database_name is
// unused — select the logical database in the URL path.
func New(conf *config.DatabaseConfig) *Driver {
	return &Driver{config: conf}
}

// ValidateConfig statically checks a redis database's connection_url shape, so
// config mistakes surface before any connection is attempted.
func ValidateConfig(conf *config.DatabaseConfig) error {
	if _, err := goredis.ParseURL(conf.ConnectionUrl); err != nil {
		return fmt.Errorf("failed to parse redis connection_url: %w", err)
	}
	return nil
}

func (d *Driver) Connect(ctx context.Context) error {
	opts, err := goredis.ParseURL(d.config.ConnectionUrl)
	if err != nil {
		return fmt.Errorf("failed to parse redis connection_url: %w", err)
	}

	client := goredis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return fmt.Errorf("failed to ping redis: %w", err)
	}

	d.client = client
	return nil
}

func (d *Driver) Disconnect(ctx context.Context) error {
	if d.client == nil {
		return nil
	}
	return d.client.Close()
}

func (d *Driver) GetAppliedMigrationsMetadata(ctx context.Context) ([]*migrationsmeta.MigrationMetadata, error) {
	data, err := d.client.Get(ctx, MIGRATIONS_KEY).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return jsontracking.Unmarshal(data)
}

func (d *Driver) SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []*migrationsmeta.MigrationMetadata) error {
	data, err := jsontracking.Marshal(migrationsMetadata)
	if err != nil {
		return err
	}
	return d.client.Set(ctx, MIGRATIONS_KEY, data, 0).Err()
}

// BeginTx is a no-op: handle operations apply immediately and are not rolled
// back on failure (see the package comment for why MULTI/EXEC is not used).
func (d *Driver) BeginTx(ctx context.Context) error { return nil }

// CommitTx is a no-op; see BeginTx.
func (d *Driver) CommitTx(ctx context.Context) error { return nil }

// RollbackTx is a no-op; see BeginTx.
func (d *Driver) RollbackTx(ctx context.Context) error { return nil }

// HasOpenTx always reports false; see BeginTx.
func (d *Driver) HasOpenTx() bool { return false }

func (d *Driver) Handle(ctx context.Context) any {
	return &Handle{ctx: ctx, driver: d}
}

func (d *Driver) Init(ctx context.Context, runtime *goja.Runtime) {}

func (d *Driver) Globals(ctx context.Context, runtime *goja.Runtime) map[string]any {
	return map[string]any{}
}

func (d *Driver) MaybeFromJSValue(ctx context.Context, jsvm *goja.Runtime, value goja.Value) (any, bool) {
	return nil, false
}

// MaybeIntoJSValue: no driver-native Go→JS conversions for redis; the generic
// reflection conversion handles everything.
func (d *Driver) MaybeIntoJSValue(ctx context.Context, jsvm *goja.Runtime, value any) (goja.Value, bool) {
	return nil, false
}
