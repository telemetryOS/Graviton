package fs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/telemetryos/graviton/driver/internal/lockretry"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"
)

// MIGRATIONS_LOCK_FILE is the whole-run migrations lock, stored next to the
// tracking document when this driver is the migrations_db.
const MIGRATIONS_LOCK_FILE = "graviton-migrations.lock"

func (d *Driver) lockPath() string {
	return filepath.Join(d.root, MIGRATIONS_LOCK_FILE)
}

// AcquireMigrationsLock claims the lock by exclusively creating the lock file
// (O_EXCL makes creation atomic on POSIX filesystems).
func (d *Driver) AcquireMigrationsLock(ctx context.Context, lock *migrationsmeta.MigrationsLock) (*migrationsmeta.MigrationsLock, error) {
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return nil, err
	}

	return lockretry.Acquire(
		func() (bool, error) {
			file, err := os.OpenFile(d.lockPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
			if os.IsExist(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if _, err := file.Write(data); err != nil {
				file.Close()
				os.Remove(d.lockPath())
				return false, err
			}
			return true, file.Close()
		},
		func() (*migrationsmeta.MigrationsLock, error) {
			return d.GetMigrationsLock(ctx)
		},
	)
}

func (d *Driver) ReleaseMigrationsLock(ctx context.Context, holder string) error {
	current, err := d.GetMigrationsLock(ctx)
	if err != nil {
		return err
	}
	if current == nil || current.Holder != holder {
		return nil
	}
	return os.Remove(d.lockPath())
}

func (d *Driver) GetMigrationsLock(ctx context.Context) (*migrationsmeta.MigrationsLock, error) {
	data, err := os.ReadFile(d.lockPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var lock migrationsmeta.MigrationsLock
	if err := json.Unmarshal(data, &lock); err != nil {
		// A torn or foreign lock file still means the lock is held; report it
		// with what little is known so unlock can clear it.
		return &migrationsmeta.MigrationsLock{Hostname: "unknown"}, nil
	}
	return &lock, nil
}

func (d *Driver) ClearMigrationsLock(ctx context.Context) error {
	err := os.Remove(d.lockPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
