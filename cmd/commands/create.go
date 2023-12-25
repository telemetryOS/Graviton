package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/telemetrytv/graviton-cli/internal/config"
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

		now := time.Now()
		timestamp := now.Format("20060102150405")
		filename := fmt.Sprintf("%s-%s.migration.ts", timestamp, name)

		migrationPath := filepath.Join(conf.ProjectPath, conf.MongoDB.MigrationsPath, filename)
		if err := os.WriteFile(migrationPath, migrations.Template, 0644); err != nil {
			panic(err)
		}
	},
}

func init() {
	rootCmd.AddCommand(createCmd)
}
