package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/telemetrytv/graviton-cli/internal/config"
	"github.com/telemetrytv/graviton-cli/internal/driver"
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

		databaseNames := TargetDatabaseNames(conf)
		for i, databaseName := range databaseNames {
			if i != 0 {
				fmt.Println("---")
			}

			databaseConf := conf.Database(databaseName)
			drv := driver.FromDatabaseConfig(databaseConf)
			if err := drv.Connect(ctx); err != nil {
				panic(err)
			}

			pendingMigrations, err := migrations.GetPending(ctx, conf.ProjectPath, databaseConf, drv)
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
				fmt.Println("target migration not found")
				os.Exit(1)
			}
			pendingMigrations = pendingMigrations[:targetMigrationIndex+1]

			fmt.Print("Applying migrations for database `" + databaseName)
			if targetMigrationName == "" {
				fmt.Println("`")
			} else {
				fmt.Println("` to `" + targetMigrationName + "`")
			}
			pendingMigrationNames := []string{}
			for _, pendingMigration := range pendingMigrations {
				pendingMigrationNames = append(pendingMigrationNames, " +++ "+pendingMigration.Name())
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

			fmt.Println("Applied migrations for database `" + databaseName)
			if targetMigrationName == "" {
				fmt.Println("`")
			} else {
				fmt.Println("` to `" + targetMigrationName + "`")
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(upCmd)
}
