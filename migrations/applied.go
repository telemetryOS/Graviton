package migrations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/telemetryos/graviton/config"
	"github.com/telemetryos/graviton/driver"
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
			Script: NewScript(
				ctx,
				d,
				d.Handle(ctx),
				appliedMigrationMetadata.Source,
				appliedMigrationMetadata.Filename,
			),
		})
	}

	return appliedMigrations, nil
}

func GetAppliedWithDownFuncFromDisk(ctx context.Context, projectPath string, conf *config.DatabaseConfig, d driver.Driver) ([]*Migration, error) {
	appliedMigrationsMetadata, err := d.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		return nil, err
	}

	migrationsPath := filepath.Join(projectPath, conf.MigrationsPath)

	var appliedMigrations []*Migration
	for _, appliedMigrationMetadata := range appliedMigrationsMetadata {
		migrationPath := filepath.Join(migrationsPath, appliedMigrationMetadata.Filename)

		stat, err := os.Stat(migrationPath)
		if err != nil {
			return nil, err
		}
		if !stat.Mode().IsRegular() {
			fmt.Println(
				"Could not collect the necessary down functions for applied migrations " +
					"on disk. Missing migration file `" + appliedMigrationMetadata.Filename +
					"` from migrations directory `" + conf.MigrationsPath + "`",
			)
			os.Exit(1)
		}

		script, err := CompileScriptFromFile(
			ctx,
			d,
			appliedMigrationMetadata.Filename,
			migrationPath,
		)
		if err != nil {
			return nil, err
		}

		appliedMigrations = append(appliedMigrations, &Migration{
			MigrationMetadata: appliedMigrationMetadata,
			Script:            script,
		})
	}

	return appliedMigrations, nil
}
