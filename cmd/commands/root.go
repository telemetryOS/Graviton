package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/telemetryos/graviton/assets"
	"github.com/telemetryos/graviton/config"
	"github.com/telemetryos/graviton/migrations"

	"github.com/spf13/cobra"
)

// Version is the Graviton version. It defaults to the released tag and can be
// overridden at build time with:
//
//	-ldflags "-X github.com/telemetryos/graviton/cmd/commands.Version=$(git describe --tags)"
var Version = "v2.1.0"

var rootCmd = &cobra.Command{
	Use:     "graviton",
	Short:   "Graviton - A migration tool",
	Long:    assets.Description,
	Version: Version,
	Args:    cobra.NoArgs,
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
		fmt.Println("No configuration found. Create a graviton.config.toml in the root of your project.")
		os.Exit(0)
	}
	if err := conf.Validate(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	return conf
}

// connectRun builds a Run over the whole project and connects every configured
// database, exiting with the connection error on failure.
func connectRun(conf *config.Config) *migrations.Run {
	run := migrations.NewRun(context.Background(), conf)
	if err := run.Connect(); err != nil {
		panic(err)
	}
	return run
}

func pendingMigrationNamesWithPrefix(conf *config.Config, prefix string) []string {
	run := migrations.NewRun(context.Background(), conf)
	if err := run.Connect(); err != nil {
		return []string{}
	}
	defer run.Disconnect()

	pendingMigrations, err := run.GetPending()
	if err != nil {
		return []string{}
	}
	return migrationNamesWithPrefix(pendingMigrations, prefix)
}

func appliedMigrationNamesWithPrefix(conf *config.Config, prefix string) []string {
	run := migrations.NewRun(context.Background(), conf)
	if err := run.Connect(); err != nil {
		return []string{}
	}
	defer run.Disconnect()

	appliedMigrations, err := run.GetApplied()
	if err != nil {
		return []string{}
	}
	return migrationNamesWithPrefix(appliedMigrations, prefix)
}

func appliedMigrationNamesFromDiskWithPrefix(conf *config.Config, prefix string) []string {
	run := migrations.NewRun(context.Background(), conf)
	if err := run.Connect(); err != nil {
		return []string{}
	}
	defer run.Disconnect()

	appliedMigrations, err := run.GetAppliedWithDownFuncFromDisk()
	if err != nil {
		return []string{}
	}
	return migrationNamesWithPrefix(appliedMigrations, prefix)
}

func allMigrationNamesWithPrefix(conf *config.Config, prefix string) []string {
	appliedMigrationNames := appliedMigrationNamesWithPrefix(conf, prefix)
	pendingMigrationNames := pendingMigrationNamesWithPrefix(conf, prefix)
	return append(appliedMigrationNames, pendingMigrationNames...)
}

func migrationNamesWithPrefix(ms []*migrations.Migration, prefix string) []string {
	migrationNames := []string{}
	for _, migration := range ms {
		if strings.HasPrefix(migration.Name(), prefix) {
			migrationNames = append(migrationNames, migration.Name())
		}
	}
	return migrationNames
}
