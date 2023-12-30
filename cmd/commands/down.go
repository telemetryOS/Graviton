package commands

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/telemetrytv/graviton-cli/internal/config"
	"github.com/telemetrytv/graviton-cli/internal/driver"
	"github.com/telemetrytv/graviton-cli/internal/migrations"
	migrationsmeta "github.com/telemetrytv/graviton-cli/internal/migrations-meta"
)

var useDownFnOnDisk bool

var downCmd = &cobra.Command{
	Use:   "down <migration>",
	Short: "reverses applied migrations up to and including the specified migration",
	Long: "Will reverse all applied migrations in order up to and including " +
		"the specified migration",
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

			var appliedMigrations []*migrations.Migration
			var err error
			if useDownFnOnDisk {
				appliedMigrations, err = migrations.GetAppliedWithDownFuncFromDisk(ctx, conf.ProjectPath, databaseConf, drv)
			} else {
				appliedMigrations, err = migrations.GetApplied(ctx, drv)
			}
			if err != nil {
				panic(err)
			}

			// NOTE: We reverse the order of the applied migrations so that we can
			// roll them back - most recent first.
			sort.Slice(appliedMigrations, func(i, j int) bool {
				return appliedMigrations[i].Filename > appliedMigrations[j].Filename
			})

			targetMigrationIndex := -1
			for i, appliedMigration := range appliedMigrations {
				if appliedMigration.Name() == targetMigrationName {
					targetMigrationIndex = i
					break
				}
			}
			if targetMigrationIndex == -1 {
				fmt.Println("target migration not found")
				os.Exit(1)
			}
			remainingMigrations := appliedMigrations[targetMigrationIndex+1:]
			appliedMigrations = appliedMigrations[:targetMigrationIndex+1]

			remainingMigrationsMetadata := []*migrationsmeta.MigrationMetadata{}
			for _, remainingMigration := range remainingMigrations {
				remainingMigrationsMetadata = append(remainingMigrationsMetadata, remainingMigration.MigrationMetadata)
			}

			fmt.Println("Reverting migrations for database `" + databaseName + "` to `" + targetMigrationName + "`")
			if useDownFnOnDisk {
				fmt.Println("WARN: Using down functions from disk")
			}
			appliedMigrationNames := []string{}
			for _, appliedMigration := range appliedMigrations {
				appliedMigrationNames = append(appliedMigrationNames, " --- "+appliedMigration.Name())
			}
			fmt.Println(strings.Join(appliedMigrationNames, "\n"))

			err = drv.WithTransaction(ctx, func() error {
				for _, appliedMigration := range appliedMigrations {
					err := appliedMigration.Script.Down()
					if err != nil {
						return err
					}

					err = drv.SetAppliedMigrationsMetadata(ctx, remainingMigrationsMetadata)
					if err != nil {
						return err
					}
				}
				return nil
			})

			if err != nil {
				panic(err)
			}

			fmt.Println("Reverted migrations for database `" + databaseName + "` to `" + targetMigrationName + "`")
		}
	},
}

func init() {
	flags := downCmd.Flags()
	flags.BoolVar(&useDownFnOnDisk, "from-disk", false, "use migrations on disk instead of migrations in the database")

	rootCmd.AddCommand(downCmd)
}
