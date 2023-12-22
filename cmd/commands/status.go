package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/telemetrytv/graviton-cli/internal/config"
	"github.com/telemetrytv/graviton-cli/internal/driver/mongodb"
	"github.com/telemetrytv/graviton-cli/internal/migrations"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "gets the migration status",
	Long:  "Shows what migrations have been applied and which ones have not",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.TODO()

		conf, err := config.Load()
		if err != nil {
			panic(err)
		}

		driver := mongodb.New()
		driver.Connect(ctx, &mongodb.Options{
			URI:      conf.MongoDB.URI,
			Database: conf.MongoDB.Database,
		})

		pendingMigrations, err := migrations.GetPending(ctx, conf, driver)
		if err != nil {
			panic(err)
		}
		pendingMigrationNames := []string{}
		for _, pendingMigration := range pendingMigrations {
			pendingMigrationNames = append(pendingMigrationNames, " - "+pendingMigration.Name)
		}

		appliedMigrations, err := migrations.GetApplied(ctx, driver)
		if err != nil {
			panic(err)
		}
		appliedMigrationNames := []string{}
		for _, appliedMigration := range appliedMigrations {
			appliedMigrationNames = append(appliedMigrationNames, " - "+appliedMigration.Name)
		}

		fmt.Println("Pending migrations:")
		if len(pendingMigrationNames) == 0 {
			fmt.Println(" - none")
		} else {
			fmt.Println(strings.Join(pendingMigrationNames, "\n"))
		}

		fmt.Println("Applied migrations:")
		if len(appliedMigrationNames) == 0 {
			fmt.Println(" - none")
		} else {
			fmt.Println(strings.Join(appliedMigrationNames, "\n"))
		}
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
