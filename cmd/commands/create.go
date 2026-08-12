package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/telemetryos/graviton/migrations"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:               "create <name>",
	Short:             "creates a new migration",
	Long:              "Creates a new migration with the specified name in the single migrations directory.",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: cobra.NoFileCompletions,
	Run: func(cmd *cobra.Command, args []string) {
		conf := assertConfig()
		migrationName := args[0]

		now := time.Now()
		timestamp := now.Format("20060102150405")
		filename := fmt.Sprintf("%s-%s.migration.ts", timestamp, migrationName)
		migrationsDir := filepath.Join(conf.ProjectPath, conf.MigrationsPath)
		migrationPath := filepath.Join(migrationsDir, filename)

		if _, err := os.Stat(migrationPath); err != nil {
			if !os.IsNotExist(err) {
				fmt.Println("Cannot create migration: " + err.Error())
				return
			}
			if err := os.MkdirAll(migrationsDir, 0755); err != nil {
				panic(err)
			}

			tsConfigPath := filepath.Join(migrationsDir, "tsconfig.json")
			if err := os.WriteFile(tsConfigPath, migrations.TSConfigTemplate, 0644); err != nil {
				panic(err)
			}

			typeDefPath := filepath.Join(migrationsDir, "migration.d.ts")
			if err := os.WriteFile(typeDefPath, migrations.MigrationTypeDefTemplate, 0644); err != nil {
				panic(err)
			}
		}

		if err := os.WriteFile(migrationPath, migrations.MigrationTemplate, 0644); err != nil {
			panic(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(createCmd)
}
