package migrations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/davecgh/go-spew/spew"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"

	"github.com/telemetrytv/graviton-cli/internal/config"
	"github.com/telemetrytv/graviton-cli/internal/driver"
)

var migrationNamePattern = regexp.MustCompile(`^m\d{12}[a-z0-9]+$`)

func Up(ctx context.Context, d driver.AppliedMigrationsStore, targetMigrationName string) error {
	migrations, err := GetApplied(ctx, d)
	if err != nil {
		return err
	}
	for _, migration := range migrations {
		if migration.Name == targetMigrationName {
			return nil
		}
	}
	return nil
}

func GetPending(ctx context.Context, store driver.AppliedMigrationsStore) ([]*Migration, error) {
	appliedMigrationsMetadata, err := store.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		return nil, err
	}
	appliedMigrationsNames := make(map[string]bool)
	for _, appliedMigrationMetadata := range appliedMigrationsMetadata {
		appliedMigrationsNames[appliedMigrationMetadata.Name] = true
	}

	c, err := config.Load()
	if err != nil {
		return nil, err
	}
	if c.MongoDB == nil {
		c.MongoDB = &config.ConfigMongoDB{}
	}
	if c.MongoDB.MigrationsPath == "" {
		c.MongoDB.MigrationsPath = "migrations"
	}

	migrationsPath := filepath.Join(c.ProjectPath, c.MongoDB.MigrationsPath)
	migrationsDir, err := os.ReadDir(migrationsPath)
	if err != nil {
		return nil, err
	}

	// var pendingMigrations []*Migration
	for _, migrationDir := range migrationsDir {
		migrationDirName := migrationDir.Name()
		if !migrationDir.IsDir() ||
			appliedMigrationsNames[migrationDirName] ||
			!migrationNamePattern.MatchString(migrationDirName) {
			fmt.Println("skipping", migrationDirName)
			continue
		}

		migrationPath := filepath.Join(migrationsPath, migrationDirName)
		interpreter := interp.New(interp.Options{
			GoPath:       filepath.Dir(migrationPath),
			Unrestricted: true,
		})
		if err := interpreter.Use(stdlib.Symbols); err != nil {
			return nil, err
		}

		value, err := interpreter.EvalPath(migrationPath)
		if err != nil {
			return nil, err
		}

		spew.Dump(value)
		// migrationExports, _ := migrationPkgVal.Interface().(interp.Exports)
		// if !ok ||
		// 	migrationExports == nil ||
		// 	migrationExports["Up"] == nil ||
		// 	migrationExports["Down"] == nil ||
		// 	migrationExports["Name"] == nil {
		// 	return nil, errors.New("migration " + migrationName + " does not have required exports. See documentation for more information.")
		// }

		// spew.Dump(migrationExports)

		// migrationUp := migrationExports["Up"][0].Interface().(func(context.Context, any) error)
	}

	return nil, nil
}

func TMP() {
	spew.Dump(gravitonlib.Migration{})
	// mongoDriver := mongodb.New()
	// mongoDriver.Connect(context.TODO(), &mongodb.Options{
	// 	URI:      "mongodb://localhost:27017",
	// 	Database: "graviton",
	// })

	// _, err := GetPending(context.TODO(), mongoDriver)
	// if err != nil {
	// 	panic(err)
	// }
}
