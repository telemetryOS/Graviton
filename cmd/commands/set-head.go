package commands

import (
	"context"
	"time"

	"graviton/internal/driver"
	"graviton/internal/migrations"
	migrationsmeta "graviton/internal/migrations-meta"

	"github.com/spf13/cobra"
)

var setHeadCmd = &cobra.Command{
	Use:   "set-head [database] <migration>",
	Short: "sets the head migration",
	Long:  "Allows setting which migrations have been applied without running them by selecting a new head",
	Args:  cobra.RangeArgs(1, 2),

	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		conf := assertConfig()
		singularDatabase := conf.GetSingularDatabase()
		switch len(args) {
		case 0:
			if singularDatabase != "" {
				migrationNames := allMigrationNamesWithPrefix(conf, singularDatabase, toComplete)
				return append(migrationNames, "-"), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
			}
			databaseNames := databaseNamesWithPrefix(conf, toComplete)
			return databaseNames, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
		case 1:
			if singularDatabase == "" {
				migrationNames := allMigrationNamesWithPrefix(conf, args[0], toComplete)
				return append(migrationNames, "-"), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
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

		if migrationName == "-" {
			if err := drv.SetAppliedMigrationsMetadata(ctx, []*migrationsmeta.MigrationMetadata{}); err != nil {
				panic(err)
			}
			return
		}

		pendingMigrations, err := migrations.GetPending(ctx, conf.ProjectPath, databaseConf, drv)
		if err != nil {
			panic(err)
		}

		appliedMigrations, err := migrations.GetApplied(ctx, drv)
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

		if err := drv.SetAppliedMigrationsMetadata(ctx, migrationsMetadata); err != nil {
			panic(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(setHeadCmd)
}
