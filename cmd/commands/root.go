package commands

import (
	"github.com/spf13/cobra"
	"github.com/telemetrytv/graviton-cli/internal/migrations"
)

var rootCmd = &cobra.Command{
	Use:   "graviton",
	Short: "Gravion - A migration tool",
	Long:  "Graviton is a migration tool. Use it to shape your data predictably",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func Execute() error {
	// return rootCmd.Execute()
	migrations.TMP()
	return nil
}
