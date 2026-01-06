package migrations

import (
	"context"
	"os"
	"path/filepath"

	"graviton/config"
	"graviton/driver"
	migrationsmeta "graviton/migrations-meta"
)

func GetPending(ctx context.Context, projectPath string, conf *config.DatabaseConfig, d driver.Driver) ([]*Migration, error) {
	appliedMigrationsMetadata, err := d.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		return nil, err
	}
	appliedMigrationsFilenames := make(map[string]bool)
	for _, appliedMigrationMetadata := range appliedMigrationsMetadata {
		appliedMigrationsFilenames[appliedMigrationMetadata.Filename] = true
	}

	if conf.MigrationsPath == "" {
		conf.MigrationsPath = "migrations"
	}

	migrationsPath := filepath.Join(projectPath, conf.MigrationsPath)
	migrationsDir, err := os.ReadDir(migrationsPath)
	if err != nil {
		return nil, err
	}

	var pendingMigrations []*Migration
	for _, migrationDir := range migrationsDir {
		migrationFilename := migrationDir.Name()
		if appliedMigrationsFilenames[migrationFilename] || !migrationDir.Type().IsRegular() || !migrationsmeta.MigrationNamePattern.MatchString(migrationFilename) {
			continue
		}

		migrationPath := filepath.Join(migrationsPath, migrationFilename)
		script, err := CompileScriptFromFile(ctx, d, migrationFilename, migrationPath)
		if err != nil {
			return nil, err
		}

		pendingMigrations = append(pendingMigrations, &Migration{
			MigrationMetadata: &migrationsmeta.MigrationMetadata{
				Filename: migrationFilename,
				Source:   script.src,
			},
			Script: script,
		})
	}

	return pendingMigrations, nil
}
