package commands

import "github.com/spf13/cobra"

var downCmd = &cobra.Command{
	Use:   "down <migration>",
	Short: "reverses applied migrations up to and including the specified migration",
	Long: "Will reverse all applied migrations in order up to and including " +
		"the specified migration",
	Run: func(cmd *cobra.Command, args []string) {
	},
}

func init() {
	rootCmd.AddCommand(downCmd)
}
