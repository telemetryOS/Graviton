package migrations

import (
	"context"

	"github.com/telemetrytv/graviton-cli/internal/driver"
)

func Down(ctx context.Context, d driver.AppliedMigrationsStore, targetMigrationName string) error {
	return nil
}

func GetApplied(ctx context.Context, driver driver.AppliedMigrationsStore) ([]*Migration, error) {
	migrationsMetadata, err := driver.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		return nil, err
	}
	migrations := make([]*Migration, len(migrationsMetadata))
	for i, migrationMetadata := range migrationsMetadata {
		migrations[i] = &Migration{MigrationMetadata: &migrationMetadata}
	}
	return migrations, nil
}

func SetApplied(ctx context.Context, d driver.AppliedMigrationsStore, migrations []*Migration) error {
	var migrationsMetadata []driver.MigrationMetadata
	for _, migration := range migrations {
		migrationsMetadata = append(migrationsMetadata, *migration.MigrationMetadata)
	}
	return d.SetAppliedMigrationsMetadata(ctx, migrationsMetadata)
}
