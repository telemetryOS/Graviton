package commands

import "github.com/spf13/cobra"

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "gets the migration status",
	Long:  "Shows what migrations have been applied and which ones have not",
	Run: func(cmd *cobra.Command, args []string) {
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
