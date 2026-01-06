package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"graviton/assets"
	"graviton/config"
	"graviton/driver"
	"graviton/migrations"

	"github.com/spf13/cobra"
)

var TargetDatabaseNamesStr string

var rootCmd = &cobra.Command{
	Use:   "graviton",
	Short: "Graviton - A migration tool",
	Long:  assets.Description,
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s\n", assets.Splash)
		cmd.Help()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func assertConfig() *config.Config {
	conf, err := config.Load()
	if err != nil {
		panic(err)
	}
	if conf == nil {
		fmt.Println("No configuration found. Create a graviton.toml in the root of your project.")
		os.Exit(0)
	}

	return conf
}

func databaseNamesWithPrefix(conf *config.Config, prefix string) []string {
	databaseNames := []string{}
	for _, database := range conf.Databases {
		if strings.HasPrefix(database.Name, prefix) {
			databaseNames = append(databaseNames, database.Name)
		}
	}
	return databaseNames
}

func pendingMigrationNamesWithPrefix(conf *config.Config, databaseName string, prefix string) []string {
	databaseConf := conf.Database(databaseName)
	drv := driver.FromDatabaseConfig(databaseConf)
	if err := drv.Connect(context.Background()); err != nil {
		return []string{}
	}
	pendingMigrations, err := migrations.GetPending(context.Background(), conf.ProjectPath, databaseConf, drv)
	if err != nil {
		return []string{}
	}
	migrationNames := []string{}
	for _, migration := range pendingMigrations {
		if strings.HasPrefix(migration.Name(), prefix) {
			migrationNames = append(migrationNames, migration.Name())
		}
	}
	return migrationNames
}

func appliedMigrationNamesWithPrefix(conf *config.Config, databaseName string, prefix string) []string {
	databaseConf := conf.Database(databaseName)
	drv := driver.FromDatabaseConfig(databaseConf)
	if err := drv.Connect(context.Background()); err != nil {
		return []string{}
	}
	pendingMigrations, err := migrations.GetApplied(context.Background(), drv)
	if err != nil {
		return []string{}
	}
	migrationNames := []string{}
	for _, migration := range pendingMigrations {
		if strings.HasPrefix(migration.Name(), prefix) {
			migrationNames = append(migrationNames, migration.Name())
		}
	}
	return migrationNames
}

func appliedMigrationNamesFromDiskWithPrefix(conf *config.Config, databaseName string, prefix string) []string {
	databaseConf := conf.Database(databaseName)
	drv := driver.FromDatabaseConfig(databaseConf)
	if err := drv.Connect(context.Background()); err != nil {
		return []string{}
	}
	pendingMigrations, err := migrations.GetAppliedWithDownFuncFromDisk(context.Background(), conf.ProjectPath, databaseConf, drv)
	if err != nil {
		return []string{}
	}
	migrationNames := []string{}
	for _, migration := range pendingMigrations {
		if strings.HasPrefix(migration.Name(), prefix) {
			migrationNames = append(migrationNames, migration.Name())
		}
	}
	return migrationNames
}

func allMigrationNamesWithPrefix(conf *config.Config, databaseName string, prefix string) []string {
	appliedMigrationNames := appliedMigrationNamesWithPrefix(conf, databaseName, prefix)
	pendingMigrationNames := pendingMigrationNamesWithPrefix(conf, databaseName, prefix)
	return append(appliedMigrationNames, pendingMigrationNames...)
}

func resolveAndAssertDBName(conf *config.Config, cmd *cobra.Command, args []string) string {
	databaseName := ""
	if len(args) == 1 {
		databaseName = args[0]
	}
	if databaseName == "" {
		databaseName = conf.GetSingularDatabase()
		if databaseName == "" {
			fmt.Println("This project is configured with multiple databases. Please specify one.")
			os.Exit(0)
		}
	}
	return databaseName
}

func resolveAndAssertDBNameAndMigration(conf *config.Config, cmd *cobra.Command, args []string) (string, string) {
	databaseName := ""
	migrationName := ""
	switch len(args) {
	case 1:
		migrationName = args[0]
	case 2:
		databaseName = args[0]
		migrationName = args[1]
	}
	if databaseName == "" {
		databaseName = conf.GetSingularDatabase()
		if databaseName == "" {
			fmt.Println("This project is configured with multiple databases. Please specify one.")
			os.Exit(0)
		}
	}
	return databaseName, migrationName
}
