package migrations

import (
	"context"
	"os"
	"path/filepath"

	"github.com/telemetrytv/graviton-cli/internal/config"
	"github.com/telemetrytv/graviton-cli/internal/driver"
)

func GetPending(ctx context.Context, conf *config.Config, d driver.Driver) ([]*Migration, error) {
	appliedMigrationsMetadata, err := d.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		return nil, err
	}
	appliedMigrationsFilenames := make(map[string]bool)
	for _, appliedMigrationMetadata := range appliedMigrationsMetadata {
		appliedMigrationsFilenames[appliedMigrationMetadata.Filename] = true
	}

	if conf.MongoDB == nil {
		conf.MongoDB = &config.ConfigMongoDB{}
	}
	if conf.MongoDB.MigrationsPath == "" {
		conf.MongoDB.MigrationsPath = "migrations"
	}

	migrationsPath := filepath.Join(conf.ProjectPath, conf.MongoDB.MigrationsPath)
	migrationsDir, err := os.ReadDir(migrationsPath)
	if err != nil {
		return nil, err
	}

	var pendingMigrations []*Migration
	for _, migrationDir := range migrationsDir {
		migrationFilename := migrationDir.Name()
		if appliedMigrationsFilenames[migrationFilename] || !migrationDir.Type().IsRegular() || !driver.MigrationNamePattern.MatchString(migrationFilename) {
			continue
		}

		migrationPath := filepath.Join(migrationsPath, migrationFilename)
		script, err := CompileScriptFromFile(ctx, conf, d.Handle(ctx), migrationFilename, migrationPath)
		if err != nil {
			return nil, err
		}

		pendingMigrations = append(pendingMigrations, &Migration{
			MigrationMetadata: &driver.MigrationMetadata{
				Filename: migrationFilename,
				Source:   script.src,
			},
			Script: script,
		})
	}

	return pendingMigrations, nil
}
