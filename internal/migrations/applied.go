package migrations

import (
	"context"

	"github.com/telemetrytv/graviton-cli/internal/driver"
)

func GetApplied(ctx context.Context, d driver.Driver) ([]*Migration, error) {
	appliedMigrationsMetadata, err := d.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		return nil, err
	}

	var appliedMigrations []*Migration
	for _, appliedMigrationMetadata := range appliedMigrationsMetadata {
		appliedMigrations = append(appliedMigrations, &Migration{
			MigrationMetadata: appliedMigrationMetadata,
			Script:            NewScript(ctx, d.Handle(ctx), appliedMigrationMetadata.Source),
		})
	}

	return appliedMigrations, nil
}
