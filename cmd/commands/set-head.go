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

		lockRun(run)
		defer run.Unlock()

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

		// Resolve the name first. Previously this loop appended as it searched and
		// simply ran off the end when nothing matched, marking every migration
		// applied — the opposite of what a mistyped name should do.
		targetIndex := findMigrationIndex(allMigrations, migrationName, "set-head")

		migrationsMetadata := []*migrationsmeta.MigrationMetadata{}
		for _, migration := range allMigrations[:targetIndex+1] {
			migrationsMetadata = append(migrationsMetadata, &migrationsmeta.MigrationMetadata{
				Filename:  migration.Filename,
				Source:    migration.Source,
				AppliedAt: time.Now(),
			})
		}

		if err := run.SetHead(migrationsMetadata); err != nil {
			panic(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(setHeadCmd)
}
