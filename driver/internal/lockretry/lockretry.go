// Package lockretry holds the acquire loop shared by every driver's
// migrations-lock implementation. Claiming a lock is a two-step observation
// in all storage flavors — atomically try to claim, and on conflict read the
// current holder — so there is always a window where the holder releases
// between the failed claim and the read. The loop retries until one round
// settles: the claim succeeds, or a stable holder is observed.
package lockretry

import (
	"errors"

	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"
)

// maxAttempts bounds the loop; more than a couple of rounds means the lock is
// churning abnormally and the caller should see an error instead of spinning.
const maxAttempts = 5

// Acquire runs try-claim / read-holder rounds. tryClaim atomically attempts
// to take the lock and reports whether it succeeded; readHolder returns the
// current holder or nil when the lock is free. Acquire returns (nil, nil) on
// success and (holder, nil) when someone else holds the lock.
func Acquire(
	tryClaim func() (bool, error),
	readHolder func() (*migrationsmeta.MigrationsLock, error),
) (*migrationsmeta.MigrationsLock, error) {
	for attempt := 0; attempt < maxAttempts; attempt++ {
		claimed, err := tryClaim()
		if err != nil {
			return nil, err
		}
		if claimed {
			return nil, nil
		}
		held, err := readHolder()
		if err != nil {
			return nil, err
		}
		if held != nil {
			return held, nil
		}
		// The holder released between the failed claim and the read; retry.
	}
	return nil, errors.New("could not acquire the migrations lock after repeated attempts")
}
