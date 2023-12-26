package migrations

import (
	"context"
	"sort"

	"github.com/telemetrytv/graviton-cli/internal/config"
	"github.com/telemetrytv/graviton-cli/internal/driver"
)

func GetApplied(ctx context.Context, conf *config.Config, d driver.Driver) ([]*Migration, error) {
	appliedMigrationsMetadata, err := d.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		return nil, err
	}

	// reverse sort
	sort.Slice(appliedMigrationsMetadata, func(i, j int) bool {
		return appliedMigrationsMetadata[i].Filename > appliedMigrationsMetadata[j].Filename
	})

	var appliedMigrations []*Migration
	for _, appliedMigrationMetadata := range appliedMigrationsMetadata {
		appliedMigrations = append(appliedMigrations, &Migration{
			MigrationMetadata: appliedMigrationMetadata,
			Script: NewScript(
				ctx,
				conf,
				d.Handle(ctx),
				appliedMigrationMetadata.Source,
				appliedMigrationMetadata.Filename,
			),
		})
	}

	return appliedMigrations, nil
}
