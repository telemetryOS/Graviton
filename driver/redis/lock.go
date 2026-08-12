package redis

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/telemetryos/graviton/driver/internal/lockretry"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	goredis "github.com/redis/go-redis/v9"
)

// MIGRATIONS_LOCK_KEY is the whole-run migrations lock, a sibling of the
// tracking key when this driver is the migrations_db.
const MIGRATIONS_LOCK_KEY = "graviton-migrations-lock"

// releaseLockScript deletes the lock only while holder still owns it; GET,
// compare, and DEL run atomically inside the script.
var releaseLockScript = goredis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if raw and cjson.decode(raw)['holder'] == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

// AcquireMigrationsLock claims the lock with SET NX, which atomically fails
// when the key already exists.
func (d *Driver) AcquireMigrationsLock(ctx context.Context, lock *migrationsmeta.MigrationsLock) (*migrationsmeta.MigrationsLock, error) {
	data, err := json.Marshal(lock)
	if err != nil {
		return nil, err
	}

	return lockretry.Acquire(
		func() (bool, error) {
			return d.client.SetNX(ctx, MIGRATIONS_LOCK_KEY, data, 0).Result()
		},
		func() (*migrationsmeta.MigrationsLock, error) {
			return d.GetMigrationsLock(ctx)
		},
	)
}

func (d *Driver) ReleaseMigrationsLock(ctx context.Context, holder string) error {
	return releaseLockScript.Run(ctx, d.client, []string{MIGRATIONS_LOCK_KEY}, holder).Err()
}

func (d *Driver) GetMigrationsLock(ctx context.Context) (*migrationsmeta.MigrationsLock, error) {
	data, err := d.client.Get(ctx, MIGRATIONS_LOCK_KEY).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var lock migrationsmeta.MigrationsLock
	if err := json.Unmarshal(data, &lock); err != nil {
		// A foreign lock value still means the lock is held; report it with
		// what little is known so unlock can clear it.
		return &migrationsmeta.MigrationsLock{Hostname: "unknown"}, nil
	}
	return &lock, nil
}

func (d *Driver) ClearMigrationsLock(ctx context.Context) error {
	return d.client.Del(ctx, MIGRATIONS_LOCK_KEY).Err()
}
