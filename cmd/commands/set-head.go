package commands

import "github.com/spf13/cobra"

var setHeadCmd = &cobra.Command{
	Use:   "set-head",
	Short: "sets the head migration",
	Long:  "Allows setting which migrations have been applied without running them by selecting a new head",
	Run: func(cmd *cobra.Command, args []string) {
	},
}

func init() {
	rootCmd.AddCommand(setHeadCmd)
}
