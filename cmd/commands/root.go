package commands

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/telemetrytv/graviton-cli/internal/config"
)

var TargetDatabaseNamesStr string

var rootCmd = &cobra.Command{
	Use:   "graviton",
	Short: "Gravion - A migration tool",
	Long:  "Graviton is a migration tool. Use it to shape your data predictably",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func TargetDatabaseNames(conf *config.Config) []string {
	if len(conf.Databases) == 0 {
		fmt.Println("No databases found in config")
		os.Exit(1)
	}

	targetNames := strings.Split(TargetDatabaseNamesStr, ",")
	databaseNames := []string{}
	for _, databaseConf := range conf.Databases {
		if targetNames[0] == "" || slices.Contains(targetNames, databaseConf.Name) {
			databaseNames = append(databaseNames, databaseConf.Name)
		}
	}
	if len(databaseNames) == 0 {
		fmt.Println("Target `" + TargetDatabaseNamesStr + "` did not match any databases from the configuration")
		os.Exit(1)
	}

	return databaseNames
}

func TargetDatabaseName(config *config.Config) string {
	if len(config.Databases) == 0 {
		fmt.Println("No databases found in config")
		os.Exit(1)
	}

	targetNames := strings.Split(TargetDatabaseNamesStr, ",")
	if len(targetNames) > 1 {
		fmt.Printf("WARN: Multiple targets specified. Using `%s`\n", targetNames[0])
	}
	targetName := targetNames[0]

	for _, databaseConf := range config.Databases {
		if targetName == "" || targetName == databaseConf.Name {
			return databaseConf.Name
		}
	}

	fmt.Println("Target `" + TargetDatabaseNamesStr + "` did not match any databases from the configuration")
	os.Exit(1)
	return ""
}

func Execute() error {
	flags := rootCmd.PersistentFlags()
	flags.StringVarP(&TargetDatabaseNamesStr, "target", "t", "", "Target database names")

	return rootCmd.Execute()
}
