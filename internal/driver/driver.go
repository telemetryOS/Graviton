package driver

import (
	"context"
)

type MigrationMetadata struct {
	Name      string `bson:"name"`
	Filename  string `bson:"filename"`
	Source    string `bson:"source"`
	AppliedAt int64  `bson:"applied_at"`
}

type Driver interface {
	GetAppliedMigrationsMetadata(ctx context.Context) ([]*MigrationMetadata, error)
	SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []*MigrationMetadata) error
	WithTransaction(ctx context.Context, fn func() error) error
	Handle(ctx context.Context) any
}
