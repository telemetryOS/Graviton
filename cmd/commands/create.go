package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/telemetrytv/graviton-cli/internal/config"
	"github.com/telemetrytv/graviton-cli/internal/driver/mongodb"
	"github.com/telemetrytv/graviton-cli/internal/migrations"
)

var createCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "creates a new migration",
	Long:  "Creates a new migration with the specified name.",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			panic("expected migration name")
		}
		name := args[0]

		conf, err := config.Load()
		if err != nil {
			panic(err)
		}
		if conf == nil {
			fmt.Println("No configuration found. Create a graviton.toml in the root of your project.")
			return
		}

		now := time.Now()
		timestamp := now.Format("20060102150405")
		filename := fmt.Sprintf("%s-%s.migration.ts", timestamp, name)
		migrationPath := filepath.Join(conf.ProjectPath, conf.MongoDB.MigrationsPath, filename)

		if _, err := os.Stat(migrationPath); err != nil {
			if !os.IsNotExist(err) {
				panic("Cannot create migration: " + err.Error())
			}
			if err := os.MkdirAll(filepath.Dir(migrationPath), 0755); err != nil {
				panic(err)
			}

			tsConfigPath := filepath.Join(conf.ProjectPath, conf.MongoDB.MigrationsPath, "tsconfig.json")
			if err := os.WriteFile(tsConfigPath, migrations.TSConfigTemplate, 0644); err != nil {
				panic(err)
			}

			typeDefPath := filepath.Join(conf.ProjectPath, conf.MongoDB.MigrationsPath, "migration.d.ts")
			if err := os.WriteFile(typeDefPath, mongodb.MigrationTypeDefTemplate, 0644); err != nil {
				panic(err)
			}
		}

		if err := os.WriteFile(migrationPath, mongodb.MigrationTemplate, 0644); err != nil {
			panic(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(createCmd)
}
