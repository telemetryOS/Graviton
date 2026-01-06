package commands

import (
	"fmt"

	"graviton/upgrade"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:               "upgrade",
	Short:             "Upgrade Graviton to the latest version",
	Args:              cobra.NoArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	Run: func(_ *cobra.Command, _ []string) {
		if err := upgrade.Upgrade(); err != nil {
			panic(fmt.Errorf("upgrade failed: %w", err))
		}
	},
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}
