package commands

import (
	"context"
	"fmt"
	"strings"

	"graviton/driver"
	"graviton/migrations"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [database]",
	Short: "gets the migration status",
	Long:  "Shows what migrations have been applied and which ones have not",
	Args:  cobra.MaximumNArgs(1),

	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			conf := assertConfig()
			singularDatabase := conf.GetSingularDatabase()
			if singularDatabase == "" {
				databaseNames := databaseNamesWithPrefix(conf, toComplete)
				return databaseNames, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
			}
		}
		return []string{}, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
	},

	Run: func(cmd *cobra.Command, args []string) {
		conf := assertConfig()
		databaseName := resolveAndAssertDBName(conf, cmd, args)
		databaseConf := conf.Database(databaseName)

		ctx := context.Background()

		fmt.Println("Migration status for database `" + databaseName + "`")

		drv := driver.FromDatabaseConfig(databaseConf)
		if err := drv.Connect(ctx); err != nil {
			panic(err)
		}
		defer drv.Disconnect(ctx)

		pendingMigrations, err := migrations.GetPending(ctx, conf.ProjectPath, databaseConf, drv)
		if err != nil {
			if err, ok := err.(*migrations.BuildScriptError); ok {
				err.Print()
				return
			}
			panic(err)
		}
		pendingMigrationNames := []string{}
		for _, pendingMigration := range pendingMigrations {
			pendingMigrationNames = append(pendingMigrationNames, "   - "+pendingMigration.Name())
		}

		appliedMigrations, err := migrations.GetApplied(ctx, drv)
		if err != nil {
			panic(err)
		}
		appliedMigrationNames := []string{}
		for _, appliedMigration := range appliedMigrations {
			appliedMigrationNames = append(appliedMigrationNames, "   - "+appliedMigration.Name())
		}

		fmt.Println("  Applied migrations:")
		if len(appliedMigrationNames) == 0 {
			fmt.Println("   - none")
		} else {
			fmt.Println(strings.Join(appliedMigrationNames, "\n"))
		}

		fmt.Println("  Pending migrations:")
		if len(pendingMigrationNames) == 0 {
			fmt.Println("   - none")
		} else {
			fmt.Println(strings.Join(pendingMigrationNames, "\n"))
		}
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
