package commands

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"github.com/telemetrytv/graviton-cli/internal/config"
	"github.com/telemetrytv/graviton-cli/internal/driver/mongodb"
	"github.com/telemetrytv/graviton-cli/internal/migrations"
	migrationsmeta "github.com/telemetrytv/graviton-cli/internal/migrations-meta"
)

var setHeadCmd = &cobra.Command{
	Use:   "set-head <migration>",
	Short: "sets the head migration",
	Long:  "Allows setting which migrations have been applied without running them by selecting a new head",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			panic("missing migration argument")
		}
		migrationName := args[0]

		ctx := context.TODO()

		conf, err := config.Load()
		if err != nil {
			panic(err)
		}

		drv := mongodb.New()
		drv.Connect(ctx, &mongodb.Options{
			URI:      conf.MongoDB.URI,
			Database: conf.MongoDB.Database,
		})

		if migrationName == "-" {
			drv.SetAppliedMigrationsMetadata(ctx, []*migrationsmeta.MigrationMetadata{})
			return
		}

		pendingMigrations, err := migrations.GetPending(ctx, conf, drv)
		if err != nil {
			panic(err)
		}

		appliedMigrations, err := migrations.GetApplied(ctx, conf, drv)
		if err != nil {
			panic(err)
		}

		allMigrations := append(appliedMigrations, pendingMigrations...)

		migrationsMetadata := []*migrationsmeta.MigrationMetadata{}
		for _, migration := range allMigrations {
			migrationsMetadata = append(migrationsMetadata, &migrationsmeta.MigrationMetadata{
				Filename:  migration.Filename,
				Source:    migration.Source,
				AppliedAt: time.Now(),
			})
			if migration.Name() == migrationName {
				break
			}
		}

		drv.SetAppliedMigrationsMetadata(ctx, migrationsMetadata)
	},
}

func init() {
	rootCmd.AddCommand(setHeadCmd)
}
