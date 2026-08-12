package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"time"

	"github.com/telemetryos/graviton/driver/internal/lockretry"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"
)

// MIGRATIONS_LOCK_TABLE holds the whole-run migrations lock as a single row
// (fixed id 1), a sibling of the tracking table when this driver is the
// migrations_db.
const MIGRATIONS_LOCK_TABLE = "graviton_migrations_lock"

//go:embed sql/create_lock_table.sql
var createLockTableSQL string

//go:embed sql/insert_lock.sql
var insertLockSQL string

//go:embed sql/get_lock.sql
var getLockSQL string

//go:embed sql/delete_lock.sql
var deleteLockSQL string

//go:embed sql/clear_lock.sql
var clearLockSQL string

// AcquireMigrationsLock claims the lock with a guarded insert
// (INSERT ... WHERE NOT EXISTS), which is atomic per statement; zero rows
// affected means the lock is held. Lock statements always run on the plain
// connection — they are immediate and never join this driver's transaction.
func (d *Driver) AcquireMigrationsLock(ctx context.Context, lock *migrationsmeta.MigrationsLock) (*migrationsmeta.MigrationsLock, error) {
	insertSQL, err := d.renderSQL(insertLockSQL)
	if err != nil {
		return nil, err
	}

	return lockretry.Acquire(
		func() (bool, error) {
			result, err := d.db.ExecContext(ctx, insertSQL,
				lock.Holder, lock.Hostname, lock.Pid, lock.AcquiredAt.Format(time.RFC3339))
			if err != nil {
				return false, err
			}
			rowsAffected, err := result.RowsAffected()
			if err != nil {
				return false, err
			}
			return rowsAffected == 1, nil
		},
		func() (*migrationsmeta.MigrationsLock, error) {
			return d.GetMigrationsLock(ctx)
		},
	)
}

// ReleaseMigrationsLock deletes the lock row only while holder still owns it;
// the filtered DELETE is atomic.
func (d *Driver) ReleaseMigrationsLock(ctx context.Context, holder string) error {
	deleteSQL, err := d.renderSQL(deleteLockSQL)
	if err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx, deleteSQL, holder)
	return err
}

func (d *Driver) GetMigrationsLock(ctx context.Context) (*migrationsmeta.MigrationsLock, error) {
	getSQL, err := d.renderSQL(getLockSQL)
	if err != nil {
		return nil, err
	}

	var lock migrationsmeta.MigrationsLock
	var acquiredAtStr string
	err = d.db.QueryRowContext(ctx, getSQL).Scan(&lock.Holder, &lock.Hostname, &lock.Pid, &acquiredAtStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	acquiredAt, err := time.Parse(time.RFC3339, acquiredAtStr)
	if err != nil {
		return nil, err
	}
	lock.AcquiredAt = acquiredAt
	return &lock, nil
}

func (d *Driver) ClearMigrationsLock(ctx context.Context) error {
	clearSQL, err := d.renderSQL(clearLockSQL)
	if err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx, clearSQL)
	return err
}
