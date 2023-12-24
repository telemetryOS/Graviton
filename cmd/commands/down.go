package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/telemetrytv/graviton-cli/internal/config"
	"github.com/telemetrytv/graviton-cli/internal/driver"
	"github.com/telemetrytv/graviton-cli/internal/driver/mongodb"
	"github.com/telemetrytv/graviton-cli/internal/migrations"
)

var downCmd = &cobra.Command{
	Use:   "down <migration>",
	Short: "reverses applied migrations up to and including the specified migration",
	Long: "Will reverse all applied migrations in order up to and including " +
		"the specified migration",
	Run: func(cmd *cobra.Command, args []string) {
		targetMigrationName := ""
		if len(args) != 0 {
			targetMigrationName = args[0]
		}

		ctx := context.TODO()

		conf, err := config.Load()
		if err != nil {
			panic(err)
		}

		drv := mongodb.New()
		drv.Connect(ctx, &mongodb.Options{
			URI:      conf.MongoDB.URI,
			Database: conf.MongoDB.Database,
		})

		appliedMigrations, err := migrations.GetApplied(ctx, conf, drv)
		if err != nil {
			panic(err)
		}

		targetMigrationIndex := -1
		for i, appliedMigration := range appliedMigrations {
			if appliedMigration.Name == targetMigrationName {
				targetMigrationIndex = i
				break
			}
		}
		if targetMigrationIndex == -1 {
			panic("target migration not found")
		}
		remainingMigrations := appliedMigrations[targetMigrationIndex+1:]
		appliedMigrations = appliedMigrations[:targetMigrationIndex+1]

		remainingMigrationsMetadata := []*driver.MigrationMetadata{}
		for _, remainingMigration := range remainingMigrations {
			remainingMigrationsMetadata = append(remainingMigrationsMetadata, remainingMigration.MigrationMetadata)
		}

		fmt.Println("Reversing migrations:")
		appliedMigrationNames := []string{}
		for _, appliedMigration := range appliedMigrations {
			appliedMigrationNames = append(appliedMigrationNames, " - "+appliedMigration.Name)
		}
		fmt.Println(strings.Join(appliedMigrationNames, "\n"))

		err = drv.WithTransaction(ctx, func() error {
			for _, appliedMigration := range appliedMigrations {
				err := appliedMigration.Script.Down()
				if err != nil {
					return err
				}

				err = drv.SetAppliedMigrationsMetadata(ctx, remainingMigrationsMetadata)
				if err != nil {
					return err
				}
			}
			return nil
		})

		if err != nil {
			panic(err)
		}

		fmt.Println("Migration complete")
	},
}

func init() {
	rootCmd.AddCommand(downCmd)
}
