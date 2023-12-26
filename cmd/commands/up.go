package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/telemetrytv/graviton-cli/internal/config"
	"github.com/telemetrytv/graviton-cli/internal/driver/mongodb"
	"github.com/telemetrytv/graviton-cli/internal/migrations"
	migrationsmeta "github.com/telemetrytv/graviton-cli/internal/migrations-meta"
)

var upCmd = &cobra.Command{
	Use:   "up [migration]",
	Short: "runs migrations",
	Long: "Will apply all unapplied migrations in order. If a migration is specified, " +
		"it will run all migrations up to and including the specified migration.",
	Run: func(cmd *cobra.Command, args []string) {
		targetMigrationName := ""
		if len(args) != 0 {
			targetMigrationName = args[0]
		}

		ctx := context.TODO()

		conf, err := config.Load()
		if err != nil {
			panic(err)
		}
		if conf == nil {
			fmt.Println("No configuration found. Create a graviton.toml in the root of your project.")
			return
		}

		drv := mongodb.New()
		drv.Connect(ctx, &mongodb.Options{
			URI:      conf.MongoDB.URI,
			Database: conf.MongoDB.Database,
		})

		pendingMigrations, err := migrations.GetPending(ctx, conf, drv)
		if err != nil {
			if err, ok := err.(*migrations.BuildScriptError); ok {
				err.Print()
				return
			}
			panic(err)
		}
		if len(pendingMigrations) == 0 {
			fmt.Println("No pending migrations")
			return
		}

		targetMigrationIndex := -1
		if targetMigrationName == "" {
			targetMigrationIndex = len(pendingMigrations) - 1
		}
		for i, pendingMigration := range pendingMigrations {
			if pendingMigration.Name() == targetMigrationName {
				targetMigrationIndex = i
				break
			}
		}
		if targetMigrationIndex == -1 {
			panic("target migration not found")
		}
		pendingMigrations = pendingMigrations[:targetMigrationIndex+1]

		fmt.Println("Applying migrations:")
		pendingMigrationNames := []string{}
		for _, pendingMigration := range pendingMigrations {
			pendingMigrationNames = append(pendingMigrationNames, " - "+pendingMigration.Name())
		}
		fmt.Println(strings.Join(pendingMigrationNames, "\n"))

		err = drv.WithTransaction(ctx, func() error {
			for _, pendingMigration := range pendingMigrations {
				if err := pendingMigration.Script.Up(); err != nil {
					panic(err)
				}
				pendingMigration.AppliedAt = time.Now()
			}

			var newlyAppliedMigrationsMetadata []*migrationsmeta.MigrationMetadata
			for _, pendingMigration := range pendingMigrations {
				newlyAppliedMigrationsMetadata = append(newlyAppliedMigrationsMetadata, pendingMigration.MigrationMetadata)
			}

			if err := drv.SetAppliedMigrationsMetadata(ctx, newlyAppliedMigrationsMetadata); err != nil {
				panic(err)
			}

			return nil
		})
		if err != nil {
			panic(err)
		}

		fmt.Println("Migration complete")
	},
}

func init() {
	rootCmd.AddCommand(upCmd)
}
