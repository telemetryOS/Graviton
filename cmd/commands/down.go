package commands

import (
	"fmt"
	"sort"
	"strings"

	"github.com/telemetryos/graviton/migrations"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"github.com/spf13/cobra"
)

var downCmd = &cobra.Command{
	Use:   "down <migration>",
	Short: "reverses applied migrations up to and including the specified migration",
	Long: "Will reverse all applied migrations in order up to and including " +
		"the specified migration",
	Args: cobra.ExactArgs(1),

	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		useDownFnOnDisk, _ := cmd.Flags().GetBool("from-disk")
		if len(args) == 0 {
			conf := assertConfig()
			if useDownFnOnDisk {
				migrationNames := appliedMigrationNamesFromDiskWithPrefix(conf, toComplete)
				return append(migrationNames, "-"), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
			}
			migrationNames := appliedMigrationNamesWithPrefix(conf, toComplete)
			return append(migrationNames, "-"), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
		}
		return []string{}, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
	},

	Run: func(cmd *cobra.Command, args []string) {
		useDownFnOnDisk, _ := cmd.Flags().GetBool("from-disk")

		conf := assertConfig()
		migrationName := args[0]

		run := connectRun(conf)
		defer run.Disconnect()

		lockRun(run)
		defer run.Unlock()

		var rollbackMigrations []*migrations.Migration
		var err error
		if useDownFnOnDisk {
			rollbackMigrations, err = run.GetAppliedWithDownFuncFromDisk()
		} else {
			rollbackMigrations, err = run.GetApplied()
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
			// A name that matches nothing exits non-zero rather than reporting
			// success having done nothing, so a script cannot mistake a typo for
			// a completed rollback.
			targetMigrationIndex := findMigrationIndex(rollbackMigrations, migrationName, "down")
			rollbackMigrations = rollbackMigrations[:targetMigrationIndex+1]
		}

		fmt.Println("Reverting migrations to `" + migrationName + "`")
		if useDownFnOnDisk {
			fmt.Println("WARN: Using down functions from disk")
		}
		rollbackMigrationNames := []string{}
		for _, rollbackMigration := range rollbackMigrations {
			rollbackMigrationNames = append(rollbackMigrationNames, " --- "+rollbackMigration.Name())
		}
		fmt.Println(strings.Join(rollbackMigrationNames, "\n"))

		appliedMetadata, err := run.AppliedMetadata()
		if err != nil {
			panic(err)
		}
		markerList := append([]*migrationsmeta.MigrationMetadata{}, appliedMetadata...)

		for _, rollbackMigration := range rollbackMigrations {
			var remaining []*migrationsmeta.MigrationMetadata
			for _, m := range markerList {
				if m.Filename != rollbackMigration.Filename {
					remaining = append(remaining, m)
				}
			}
			markerList = remaining

			if err := run.ApplyMigration(rollbackMigration.Script.Down, markerList); err != nil {
				panic(err)
			}
		}

		fmt.Println("Reverted migrations to `" + migrationName + "`")
	},
}

func init() {
	downCmd.Flags().Bool("from-disk", false, "use migrations on disk instead of migrations in the database")

	rootCmd.AddCommand(downCmd)
}
