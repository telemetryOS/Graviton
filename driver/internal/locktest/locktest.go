// Package locktest exercises the migrations-lock contract shared by every
// driver. Each driver's test suite runs Run against its real storage, so the
// same acquire/conflict/release/clear semantics are proven per backend.
package locktest

import (
	"context"
	"testing"

	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"
)

// Locker is the migrations-lock slice of driver.Driver.
type Locker interface {
	AcquireMigrationsLock(ctx context.Context, lock *migrationsmeta.MigrationsLock) (*migrationsmeta.MigrationsLock, error)
	ReleaseMigrationsLock(ctx context.Context, holder string) error
	GetMigrationsLock(ctx context.Context) (*migrationsmeta.MigrationsLock, error)
	ClearMigrationsLock(ctx context.Context) error
}

// Run drives a connected driver through the full lock contract.
func Run(t *testing.T, ctx context.Context, locker Locker) {
	t.Helper()

	if err := locker.ClearMigrationsLock(ctx); err != nil {
		t.Fatalf("ClearMigrationsLock() to start clean: %v", err)
	}
	if lock, err := locker.GetMigrationsLock(ctx); err != nil || lock != nil {
		t.Fatalf("GetMigrationsLock() before acquire = %+v, %v; want nil, nil", lock, err)
	}

	// First acquire succeeds.
	mine := migrationsmeta.NewMigrationsLock()
	held, err := locker.AcquireMigrationsLock(ctx, mine)
	if err != nil || held != nil {
		t.Fatalf("first AcquireMigrationsLock() = %+v, %v; want nil, nil", held, err)
	}

	// A second acquire reports the first holder's identity.
	theirs := migrationsmeta.NewMigrationsLock()
	held, err = locker.AcquireMigrationsLock(ctx, theirs)
	if err != nil {
		t.Fatalf("second AcquireMigrationsLock() error = %v", err)
	}
	if held == nil || held.Holder != mine.Holder {
		t.Fatalf("second AcquireMigrationsLock() held = %+v, want holder %q", held, mine.Holder)
	}
	if held.Hostname != mine.Hostname || held.Pid != mine.Pid {
		t.Errorf("held lock identity = %s/%d, want %s/%d", held.Hostname, held.Pid, mine.Hostname, mine.Pid)
	}
	if held.AcquiredAt.IsZero() {
		t.Error("held lock has no acquired_at timestamp")
	}

	// Releasing with the wrong holder must not free the lock.
	if err := locker.ReleaseMigrationsLock(ctx, theirs.Holder); err != nil {
		t.Fatalf("wrong-holder ReleaseMigrationsLock() error = %v", err)
	}
	if lock, _ := locker.GetMigrationsLock(ctx); lock == nil || lock.Holder != mine.Holder {
		t.Fatal("wrong-holder release freed the lock")
	}

	// Releasing with the right holder frees it.
	if err := locker.ReleaseMigrationsLock(ctx, mine.Holder); err != nil {
		t.Fatalf("ReleaseMigrationsLock() error = %v", err)
	}
	if lock, err := locker.GetMigrationsLock(ctx); err != nil || lock != nil {
		t.Fatalf("GetMigrationsLock() after release = %+v, %v; want nil, nil", lock, err)
	}

	// Clear removes an abandoned lock unconditionally, and the lock can be
	// taken again afterwards.
	if held, err := locker.AcquireMigrationsLock(ctx, theirs); err != nil || held != nil {
		t.Fatalf("re-acquire for clear test = %+v, %v; want nil, nil", held, err)
	}
	if err := locker.ClearMigrationsLock(ctx); err != nil {
		t.Fatalf("ClearMigrationsLock() error = %v", err)
	}
	if lock, _ := locker.GetMigrationsLock(ctx); lock != nil {
		t.Fatal("lock survives ClearMigrationsLock()")
	}
	if held, err := locker.AcquireMigrationsLock(ctx, mine); err != nil || held != nil {
		t.Fatalf("acquire after clear = %+v, %v; want nil, nil", held, err)
	}
	if err := locker.ReleaseMigrationsLock(ctx, mine.Holder); err != nil {
		t.Fatalf("final ReleaseMigrationsLock() error = %v", err)
	}
}
