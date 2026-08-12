package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/telemetryos/graviton/migrations"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"github.com/spf13/cobra"
)

var upCmd = &cobra.Command{
	Use:   "up [migration]",
	Short: "runs migrations",
	Long: "Will apply all unapplied migrations in order. If a migration is specified, " +
		"it will run all migrations up to and including the specified migration.",
	Args: cobra.MaximumNArgs(1),

	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			conf := assertConfig()
			migrationNames := pendingMigrationNamesWithPrefix(conf, toComplete)
			return migrationNames, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
		}
		return []string{}, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
	},

	Run: func(cmd *cobra.Command, args []string) {
		conf := assertConfig()

		migrationName := ""
		if len(args) == 1 {
			migrationName = args[0]
		}

		run := connectRun(conf)
		defer run.Disconnect()

		lockRun(run)
		defer run.Unlock()

		applyMigrations, err := run.GetPending()
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

		if migrationName == "" {
			fmt.Println("Applying migrations")
		} else {
			fmt.Println("Applying migrations to `" + migrationName + "`")
		}
		applyMigrationNames := []string{}
		for _, applyMigration := range applyMigrations {
			applyMigrationNames = append(applyMigrationNames, " +++ "+applyMigration.Name())
		}
		fmt.Println(strings.Join(applyMigrationNames, "\n"))

		appliedMetadata, err := run.AppliedMetadata()
		if err != nil {
			panic(err)
		}
		markerList := append([]*migrationsmeta.MigrationMetadata{}, appliedMetadata...)

		for _, applyMigration := range applyMigrations {
			applyMigration.AppliedAt = time.Now()
			markerList = append(markerList, applyMigration.MigrationMetadata)

			if err := run.ApplyMigration(applyMigration.Script.Up, markerList); err != nil {
				panic(err)
			}
		}

		if migrationName == "" {
			fmt.Println("Applied migrations")
		} else {
			fmt.Println("Applied migrations to `" + migrationName + "`")
		}
	},
}

func init() {
	rootCmd.AddCommand(upCmd)
}
