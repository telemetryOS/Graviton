package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/telemetryos/graviton/driver"
	"github.com/telemetryos/graviton/migrations"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"github.com/spf13/cobra"
)

var downCmd = &cobra.Command{
	Use:   "down [database] <migration>",
	Short: "reverses applied migrations up to and including the specified migration",
	Long: "Will reverse all applied migrations in order up to and including " +
		"the specified migration",
	Args: cobra.RangeArgs(1, 2),

	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		useDownFnOnDisk, _ := cmd.Flags().GetBool("from-disk")
		conf := assertConfig()
		singularDatabase := conf.GetSingularDatabase()
		switch len(args) {
		case 0:
			if singularDatabase != "" {
				if useDownFnOnDisk {
					migrationNames := appliedMigrationNamesFromDiskWithPrefix(conf, singularDatabase, toComplete)
					return append(migrationNames, "-"), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
				}
				migrationNames := appliedMigrationNamesWithPrefix(conf, singularDatabase, toComplete)
				return append(migrationNames, "-"), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
			}
			databaseNames := databaseNamesWithPrefix(conf, toComplete)
			return databaseNames, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
		case 1:
			if singularDatabase == "" {
				if useDownFnOnDisk {
					migrationNames := appliedMigrationNamesFromDiskWithPrefix(conf, args[0], toComplete)
					return append(migrationNames, "-"), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
				}
				migrationNames := appliedMigrationNamesWithPrefix(conf, args[0], toComplete)
				return append(migrationNames, "-"), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
			}
		}
		return []string{}, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
	},

	Run: func(cmd *cobra.Command, args []string) {
		useDownFnOnDisk, _ := cmd.Flags().GetBool("from-disk")

		conf := assertConfig()
		databaseName, migrationName := resolveAndAssertDBNameAndMigration(conf, cmd, args)
		databaseConf := conf.Database(databaseName)

		ctx := context.Background()

		drv := driver.FromDatabaseConfig(databaseConf)
		if err := drv.Connect(ctx); err != nil {
			panic(err)
		}
		defer drv.Disconnect(ctx)

		var rollbackMigrations []*migrations.Migration
		var err error
		if useDownFnOnDisk {
			rollbackMigrations, err = migrations.GetAppliedWithDownFuncFromDisk(ctx, conf.ProjectPath, databaseConf, drv)
		} else {
			rollbackMigrations, err = migrations.GetApplied(ctx, drv)
		}
		if err != nil {
			panic(err)
		}

		// NOTE: We reverse the order of the applied migrations so that we can
		// roll them back - most recent first.
		sort.Slice(rollbackMigrations, func(i, j int) bool {
			return rollbackMigrations[i].Filename > rollbackMigrations[j].Filename
		})

		if migrationName != "-" {
			targetMigrationIndex := -1
			for i, appliedMigration := range rollbackMigrations {
				if appliedMigration.Name() == migrationName {
					targetMigrationIndex = i
					break
				}
			}
			if targetMigrationIndex == -1 {
				fmt.Println("target migration not found")
				return
			}

			rollbackMigrations = rollbackMigrations[:targetMigrationIndex+1]
		}

		fmt.Println("Reverting migrations for database `" + databaseName + "` to `" + migrationName + "`")
		if useDownFnOnDisk {
			fmt.Println("WARN: Using down functions from disk")
		}
		rollbackMigrationNames := []string{}
		for _, rollbackMigration := range rollbackMigrations {
			rollbackMigrationNames = append(rollbackMigrationNames, " --- "+rollbackMigration.Name())
		}
		fmt.Println(strings.Join(rollbackMigrationNames, "\n"))

		for _, rollbackMigration := range rollbackMigrations {
			err = drv.WithTransaction(ctx, func(sessCtx context.Context) error {
				err := rollbackMigration.Script.Down()
				if err != nil {
					return err
				}

				currentApplied, err := drv.GetAppliedMigrationsMetadata(sessCtx)
				if err != nil {
					return err
				}

				var updatedApplied []*migrationsmeta.MigrationMetadata
				for _, m := range currentApplied {
					if m.Filename != rollbackMigration.Filename {
						updatedApplied = append(updatedApplied, m)
					}
				}

				if err := drv.SetAppliedMigrationsMetadata(sessCtx, updatedApplied); err != nil {
					return err
				}

				return nil
			})
			if err != nil {
				panic(err)
			}
		}

		fmt.Println("Reverted migrations for database `" + databaseName + "` to `" + migrationName + "`")
	},
}

func init() {
	downCmd.Flags().Bool("from-disk", false, "use migrations on disk instead of migrations in the database")

	rootCmd.AddCommand(downCmd)
}
