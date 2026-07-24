package commands

import (
	"fmt"
	"strings"

	"github.com/telemetryos/graviton/migrations"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "gets the migration status",
	Long:  "Shows what migrations have been applied and which ones have not",
	Args:  cobra.NoArgs,

	Run: func(cmd *cobra.Command, args []string) {
		conf := assertConfig()

		run := connectRun(conf)
		defer run.Disconnect()

		fmt.Println("Migration status")

		pendingMigrations, err := run.GetPending()
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

		appliedMigrations, err := run.GetApplied()
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
