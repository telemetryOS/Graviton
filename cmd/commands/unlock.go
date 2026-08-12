package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/telemetryos/graviton/migrations"

	"github.com/spf13/cobra"
)

var unlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "clears the migrations lock left behind by a crashed run",
	Long: "Removes the whole-run migrations lock from the tracking database. The lock is " +
		"taken by up, down, and set-head and released when they finish; it only needs manual " +
		"clearing when a run died without releasing it. Never unlock while a migration run " +
		"is still alive — the lock is what keeps concurrent runs from corrupting migration " +
		"tracking.",
	Args:              cobra.NoArgs,
	ValidArgsFunction: cobra.NoFileCompletions,
	Run: func(cmd *cobra.Command, args []string) {
		conf := assertConfig()

		run := migrations.NewRun(context.Background(), conf)
		if err := run.ConnectTracking(); err != nil {
			panic(err)
		}
		defer run.DisconnectTracking()

		lock, err := run.LockInfo()
		if err != nil {
			panic(err)
		}
		if lock == nil {
			fmt.Println("No migrations lock is held")
			return
		}

		fmt.Printf(
			"Clearing migrations lock held by %s (pid %d) since %s\n",
			lock.Hostname, lock.Pid, lock.AcquiredAt.Local().Format(time.RFC3339),
		)
		if err := run.ClearLock(); err != nil {
			panic(err)
		}
		fmt.Println("Migrations lock cleared")
	},
}

func init() {
	rootCmd.AddCommand(unlockCmd)
}
