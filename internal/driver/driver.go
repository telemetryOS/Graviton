package driver

import "context"

type MigrationMetadata struct {
	Name           string `bson:"name"`
	Filename       string `bson:"filename"`
	OriginalSource string `bson:"original_source"`
	AppliedAt      int64  `bson:"applied_at"`
}

type AppliedMigrationsStore interface {
	GetAppliedMigrationsMetadata(ctx context.Context) ([]MigrationMetadata, error)
	SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []MigrationMetadata) error
}
