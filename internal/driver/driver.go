package driver

import (
	"context"
	"regexp"
)

// 000000000000-test.migration.ts
var MigrationNamePattern = regexp.MustCompile(`^\d{14}-([a-zA-Z-_]+)\.migration\.ts$`)

type MigrationMetadata struct {
	Filename  string `bson:"filename"`
	Source    string `bson:"source"`
	AppliedAt int64  `bson:"applied_at"`
}

func (m *MigrationMetadata) Name() string {
	matches := MigrationNamePattern.FindStringSubmatch(m.Filename)
	return matches[1]
}

type Driver interface {
	GetAppliedMigrationsMetadata(ctx context.Context) ([]*MigrationMetadata, error)
	SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []*MigrationMetadata) error
	WithTransaction(ctx context.Context, fn func() error) error
	Handle(ctx context.Context) any
}
