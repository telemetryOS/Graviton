package s3

import (
	"testing"

	"github.com/telemetryos/graviton/driver/internal/locktest"
)

func Test_Driver_MigrationsLock(t *testing.T) {
	drv, _, ctx := setupTestDriver(t, "s3://bucket/data")
	locktest.Run(t, ctx, drv)
}
