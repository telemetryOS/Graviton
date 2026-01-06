package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/telemetryos/graviton/driver"
	"github.com/telemetryos/graviton/migrations"

	"github.com/spf13/cobra"
)

var upCmd = &cobra.Command{
	Use:   "up [database] [migration]",
	Short: "runs migrations",
	Long: "Will apply all unapplied migrations in order. If a migration is specified, " +
		"it will run all migrations up to and including the specified migration.",
	Args: cobra.MaximumNArgs(2),

	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		conf := assertConfig()
		singularDatabase := conf.GetSingularDatabase()
		switch len(args) {
		case 0:
			if singularDatabase != "" {
				migrationNames := pendingMigrationNamesWithPrefix(conf, singularDatabase, toComplete)
				return migrationNames, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
			}
			databaseNames := databaseNamesWithPrefix(conf, toComplete)
			return databaseNames, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
		case 1:
			if singularDatabase == "" {
				migrationNames := pendingMigrationNamesWithPrefix(conf, args[0], toComplete)
				return migrationNames, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
			}
		}
		return []string{}, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
	},

	Run: func(cmd *cobra.Command, args []string) {
		conf := assertConfig()
		databaseName, migrationName := resolveAndAssertDBNameAndMigration(conf, cmd, args)
		databaseConf := conf.Database(databaseName)

		ctx := context.Background()

		drv := driver.FromDatabaseConfig(databaseConf)
		if err := drv.Connect(ctx); err != nil {
			panic(err)
		}
		defer drv.Disconnect(ctx)

		applyMigrations, err := migrations.GetPending(ctx, conf.ProjectPath, databaseConf, drv)
		if err != nil {
			if err, ok := err.(*migrations.BuildScriptError); ok {
				err.Print()
				return
			}
			panic(err)
		}
		if len(applyMigrations) == 0 {
			fmt.Println("No pending migrations")
			return
		}

		targetMigrationIndex := -1
		if migrationName == "" {
			targetMigrationIndex = len(applyMigrations) - 1
		}
		for i, pendingMigration := range applyMigrations {
			if pendingMigration.Name() == migrationName {
				targetMigrationIndex = i
				break
			}
		}
		if targetMigrationIndex == -1 {
			fmt.Println("target migration not found")
			return
		}
		applyMigrations = applyMigrations[:targetMigrationIndex+1]

		fmt.Print("Applying migrations for database `" + databaseName)
		if migrationName == "" {
			fmt.Println("`")
		} else {
			fmt.Println("` to `" + migrationName + "`")
		}
		applyMigrationNames := []string{}
		for _, applyMigration := range applyMigrations {
			applyMigrationNames = append(applyMigrationNames, " +++ "+applyMigration.Name())
		}
		fmt.Println(strings.Join(applyMigrationNames, "\n"))

		for _, applyMigration := range applyMigrations {
			err = drv.WithTransaction(ctx, func(sessCtx context.Context) error {
				if err := applyMigration.Script.Up(); err != nil {
					return err
				}

				applyMigration.AppliedAt = time.Now()

				previouslyApplied, err := drv.GetAppliedMigrationsMetadata(sessCtx)
				if err != nil {
					return err
				}

				allAppliedMigrations := append(previouslyApplied, applyMigration.MigrationMetadata)

				if err := drv.SetAppliedMigrationsMetadata(sessCtx, allAppliedMigrations); err != nil {
					return err
				}

				return nil
			})
			if err != nil {
				panic(err)
			}
		}

		fmt.Print("Applied migrations for database `" + databaseName)
		if migrationName == "" {
			fmt.Println("`")
		} else {
			fmt.Println("` to `" + migrationName + "`")
		}
	},
}

func init() {
	rootCmd.AddCommand(upCmd)
}
