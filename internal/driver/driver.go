package driver

import (
	"context"

	migrationsmeta "github.com/telemetrytv/graviton-cli/internal/migrations-meta"
)

type Driver interface {
	GetAppliedMigrationsMetadata(ctx context.Context) ([]*migrationsmeta.MigrationMetadata, error)
	SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []*migrationsmeta.MigrationMetadata) error
	WithTransaction(ctx context.Context, fn func() error) error
	Handle(ctx context.Context) any
}
