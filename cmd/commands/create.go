package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/telemetrytv/graviton-cli/internal/driver/mongodb"
	"github.com/telemetrytv/graviton-cli/internal/migrations"
)

var createCmd = &cobra.Command{
	Use:   "create [database] <name>",
	Short: "creates a new migration",
	Long:  "Creates a new migration with the specified name.",
	Args:  cobra.RangeArgs(1, 2),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			conf := assertConfig()
			databaseNames := []string{}
			for _, database := range conf.Databases {
				if strings.HasPrefix(database.Name, toComplete) {
					databaseNames = append(databaseNames, database.Name)
				}
			}
			return databaseNames, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
		}
		return []string{}, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
	},
	Run: func(cmd *cobra.Command, args []string) {
		conf := assertConfig()
		databaseName, migrationName := resolveAndAssertDBNameAndMigration(conf, cmd, args)
		databaseConf := conf.Database(databaseName)

		now := time.Now()
		timestamp := now.Format("20060102150405")
		filename := fmt.Sprintf("%s-%s.migration.ts", timestamp, migrationName)
		migrationPath := filepath.Join(conf.ProjectPath, databaseConf.MigrationsPath, filename)

		if _, err := os.Stat(migrationPath); err != nil {
			if !os.IsNotExist(err) {
				fmt.Println("Cannot create migration: " + err.Error())
				return
			}
			if err := os.MkdirAll(filepath.Dir(migrationPath), 0755); err != nil {
				panic(err)
			}

			tsConfigPath := filepath.Join(conf.ProjectPath, databaseConf.MigrationsPath, "tsconfig.json")
			if err := os.WriteFile(tsConfigPath, migrations.TSConfigTemplate, 0644); err != nil {
				panic(err)
			}

			typeDefPath := filepath.Join(conf.ProjectPath, databaseConf.MigrationsPath, "migration.d.ts")
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
