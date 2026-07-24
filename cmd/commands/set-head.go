package commands

import (
	"time"

	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"github.com/spf13/cobra"
)

var setHeadCmd = &cobra.Command{
	Use:   "set-head <migration>",
	Short: "sets the head migration",
	Long:  "Allows setting which migrations have been applied without running them by selecting a new head",
	Args:  cobra.ExactArgs(1),

	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			conf := assertConfig()
			migrationNames := allMigrationNamesWithPrefix(conf, toComplete)
			return append(migrationNames, "-"), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
		}
		return []string{}, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
	},

	Run: func(cmd *cobra.Command, args []string) {
		conf := assertConfig()
		migrationName := args[0]

		run := connectRun(conf)
		defer run.Disconnect()

		if migrationName == "-" {
			if err := run.SetHead([]*migrationsmeta.MigrationMetadata{}); err != nil {
				panic(err)
			}
			return
		}

		pendingMigrations, err := run.GetPending()
		if err != nil {
			panic(err)
		}

		appliedMigrations, err := run.GetApplied()
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

		if err := run.SetHead(migrationsMetadata); err != nil {
			panic(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(setHeadCmd)
}
