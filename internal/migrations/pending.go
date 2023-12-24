package migrations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/telemetrytv/graviton-cli/internal/config"
	"github.com/telemetrytv/graviton-cli/internal/driver"
)

// 000000000000-test.migration.ts
var migrationNamePattern = regexp.MustCompile(`^\d{12}-[a-zA-Z-_]+\.migration\.ts$`)

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
		if appliedMigrationsFilenames[migrationFilename] ||
			!migrationDir.Type().IsRegular() ||
			!migrationNamePattern.MatchString(migrationFilename) {
			fmt.Println("skipping", migrationFilename)
			continue
		}

		migrationPath := filepath.Join(migrationsPath, migrationFilename)
		script, err := BuildScriptFromFile(ctx, conf, d.Handle(ctx), migrationFilename, migrationPath)
		if err != nil {
			return nil, err
		}

		name, err := script.Name()
		if err != nil {
			return nil, err
		}

		pendingMigrations = append(pendingMigrations, &Migration{
			MigrationMetadata: &driver.MigrationMetadata{
				Name:     name,
				Filename: migrationFilename,
				Source:   script.src,
			},
			Script: script,
		})
	}

	return pendingMigrations, nil
}
