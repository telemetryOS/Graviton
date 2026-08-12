package mysql

import (
	"testing"

	"github.com/telemetryos/graviton/driver/internal/locktest"
)

func Test_Driver_MigrationsLock(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	locktest.Run(t, ctx, drv)
}
