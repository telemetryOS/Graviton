package migrations

// Live store-driver tests: a real migration script driven through goja against
// the fs and redis drivers, proving the unified handle's store surfaces
// (read/write/readBytes ArrayBuffer round-trips, get/set/hashes/command) and
// fs-hosted applied-migration tracking. The s3 handle is covered by unit tests
// in driver/s3 (its run-level path is identical to fs modulo the client).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/telemetryos/graviton/config"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	goredis "github.com/redis/go-redis/v9"
)

// Logical database 14, not 15: driver/redis uses 15, and `go test ./...`
// runs packages in parallel, so sharing an index makes each package flush
// the other's keys mid-test. That surfaced as empty lists, partial key
// counts and "no such key" — failures that read like driver bugs.
const testRedisURL = "redis://localhost:6379/14"

// setupStoresRun builds a run over an fs database (also the migrations_db) and
// a redis database, skipping when no local Redis/Valkey is available.
func setupStoresRun(t *testing.T) (*Run, string, string, *goredis.Client) {
	t.Helper()

	redisURL := testRedisURL
	if fromEnv := os.Getenv("GRAVITON_TEST_REDIS_URL"); fromEnv != "" {
		redisURL = fromEnv
	}

	projectDir := t.TempDir()
	migrationsDir := filepath.Join(projectDir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0755); err != nil {
		t.Fatalf("mkdir migrations: %v", err)
	}
	filesRoot := filepath.Join(projectDir, "files")

	conf := &config.Config{
		ProjectPath:    projectDir,
		MigrationsDb:   "files",
		MigrationsPath: "migrations",
		Databases: []*config.DatabaseConfig{
			{Name: "files", Kind: config.DatabaseKindFS, ConnectionUrl: filesRoot},
			{Name: "archive", Kind: config.DatabaseKindFS, ConnectionUrl: filepath.Join(projectDir, "archive")},
			{Name: "cache", Kind: config.DatabaseKindRedis, ConnectionUrl: redisURL},
		},
	}

	run := NewRun(context.Background(), conf)
	if err := run.Connect(); err != nil {
		t.Skipf("Redis not available on localhost: %v", err)
	}

	opts, err := goredis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	client := goredis.NewClient(opts)
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush redis test db: %v", err)
	}

	t.Cleanup(func() {
		client.FlushDB(context.Background())
		client.Close()
		run.Disconnect()
	})

	return run, migrationsDir, filesRoot, client
}

const storesUp = `
export function up(g) {
  const files = g.use('files')
  const cache = g.use('cache')

  files.write('assets/hello.txt', 'hello world')
  const content = files.read('assets/hello.txt')
  cache.set('greeting', content)
  cache.set('ephemeral', 'x', 120)
  cache.hSet('user:1', 'name', 'Alice')

  // Binary round trip: readBytes -> ArrayBuffer -> write.
  const bytes = files.readBytes('assets/hello.txt')
  files.write('assets/copy.bin', bytes)

  // Generic command escape hatch.
  cache.command('RENAME', 'ephemeral', 'renamed')

  if (!files.exists('assets/copy.bin')) throw new Error('copy.bin missing')
  if (cache.get('missing-key') !== null) throw new Error('miss should be null')
}
export function down(g) {
  g.use('files').removeAll('assets')
  g.use('cache').del('greeting', 'renamed')
  g.use('cache').command('DEL', 'user:1')
}
`

func Test_Live_Stores_UpDown(t *testing.T) {
	run, migrationsDir, filesRoot, client := setupStoresRun(t)
	writeMigration(t, migrationsDir, "20240101000000-stores.migration.ts", storesUp)

	pending, err := run.GetPending()
	if err != nil {
		t.Fatalf("GetPending() error = %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("GetPending() returned %d migrations, want 1", len(pending))
	}

	m := pending[0]
	m.AppliedAt = time.Now()
	if err := run.ApplyMigration(m.Script.Up, []*migrationsmeta.MigrationMetadata{m.MigrationMetadata}); err != nil {
		t.Fatalf("ApplyMigration(up) error = %v", err)
	}

	// File writes landed under the configured root.
	content, err := os.ReadFile(filepath.Join(filesRoot, "assets", "hello.txt"))
	if err != nil || string(content) != "hello world" {
		t.Errorf("hello.txt = %q, %v; want \"hello world\"", content, err)
	}
	copied, err := os.ReadFile(filepath.Join(filesRoot, "assets", "copy.bin"))
	if err != nil || string(copied) != "hello world" {
		t.Errorf("copy.bin = %q, %v; want ArrayBuffer round trip of \"hello world\"", copied, err)
	}

	// Redis writes landed, including TTL and the renamed key.
	ctx := context.Background()
	if got, _ := client.Get(ctx, "greeting").Result(); got != "hello world" {
		t.Errorf("greeting = %q, want \"hello world\"", got)
	}
	if got, _ := client.HGet(ctx, "user:1", "name").Result(); got != "Alice" {
		t.Errorf("user:1 name = %q, want Alice", got)
	}
	if ttl, _ := client.TTL(ctx, "renamed").Result(); ttl <= 0 {
		t.Errorf("renamed TTL = %v, want > 0 (set with TTL then RENAMEd)", ttl)
	}

	// The applied marker is tracked in the fs migrations_db.
	applied, err := run.GetApplied()
	if err != nil {
		t.Fatalf("GetApplied() error = %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied = %d, want 1", len(applied))
	}
	if _, err := os.Stat(filepath.Join(filesRoot, "graviton-migrations.json")); err != nil {
		t.Errorf("tracking document missing from fs root: %v", err)
	}

	// down: store writes are cleaned up and the marker list emptied.
	if err := run.ApplyMigration(applied[0].Script.Down, []*migrationsmeta.MigrationMetadata{}); err != nil {
		t.Fatalf("ApplyMigration(down) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(filesRoot, "assets")); !os.IsNotExist(err) {
		t.Errorf("assets directory should be removed by down(), stat err = %v", err)
	}
	if n, _ := client.Exists(ctx, "greeting", "renamed", "user:1").Result(); n != 0 {
		t.Errorf("%d redis keys survive down(), want 0", n)
	}
	if applied, _ := run.GetApplied(); len(applied) != 0 {
		t.Errorf("applied after down = %d, want 0", len(applied))
	}
}

const storesUnsupportedOp = `
export function up(g) {
  g.use('cache').collection('nope')
}
export function down(g) {}
`

func Test_Live_Stores_UnsupportedOperationErrors(t *testing.T) {
	run, migrationsDir, _, _ := setupStoresRun(t)
	writeMigration(t, migrationsDir, "20240101000000-bad-op.migration.ts", storesUnsupportedOp)

	pending, err := run.GetPending()
	if err != nil {
		t.Fatalf("GetPending() error = %v", err)
	}
	m := pending[0]
	m.AppliedAt = time.Now()

	err = run.ApplyMigration(m.Script.Up, []*migrationsmeta.MigrationMetadata{m.MigrationMetadata})
	if err == nil {
		t.Fatal("ApplyMigration() error = nil, want unsupported-operation error")
	}
	if !strings.Contains(err.Error(), "collection() is not available") {
		t.Errorf("error = %v, want a clear collection()-not-available message", err)
	}

	if applied, _ := run.GetApplied(); len(applied) != 0 {
		t.Errorf("marker written despite body failure; applied = %d, want 0", len(applied))
	}
}

const storesCopyTo = `
export function up(g) {
  g.use('files').write('exports/report.csv', 'a,b,c')
  g.use('files').copyTo('archive', 'exports/report.csv', '2024/report.csv')
}
export function down(g) {
  g.use('archive').removeAll('2024')
  g.use('files').removeAll('exports')
}
`

func Test_Live_Stores_CopyTo(t *testing.T) {
	run, migrationsDir, _, _ := setupStoresRun(t)
	writeMigration(t, migrationsDir, "20240101000000-copy-to.migration.ts", storesCopyTo)

	pending, err := run.GetPending()
	if err != nil {
		t.Fatalf("GetPending() error = %v", err)
	}
	m := pending[0]
	m.AppliedAt = time.Now()
	if err := run.ApplyMigration(m.Script.Up, []*migrationsmeta.MigrationMetadata{m.MigrationMetadata}); err != nil {
		t.Fatalf("ApplyMigration() error = %v", err)
	}

	copied, err := os.ReadFile(filepath.Join(run.conf.ProjectPath, "archive", "2024", "report.csv"))
	if err != nil || string(copied) != "a,b,c" {
		t.Errorf("copied content = %q, %v; want a,b,c", copied, err)
	}
}

const storesCopyToBadDest = `
export function up(g) {
  g.use('files').write('x.txt', 'x')
  g.use('files').copyTo('cache', 'x.txt', 'x')
}
export function down(g) {}
`

func Test_Live_Stores_CopyTo_NonStreamableDestErrors(t *testing.T) {
	run, migrationsDir, _, _ := setupStoresRun(t)
	writeMigration(t, migrationsDir, "20240101000000-copy-bad.migration.ts", storesCopyToBadDest)

	pending, err := run.GetPending()
	if err != nil {
		t.Fatalf("GetPending() error = %v", err)
	}
	m := pending[0]
	m.AppliedAt = time.Now()

	err = run.ApplyMigration(m.Script.Up, []*migrationsmeta.MigrationMetadata{m.MigrationMetadata})
	if err == nil {
		t.Fatal("ApplyMigration() error = nil, want non-streamable destination error")
	}
	if !strings.Contains(err.Error(), "destination must be an fs or s3 database") {
		t.Errorf("error = %v, want the destination-kind message", err)
	}
}

const storesNullArg = `
export function up(g) {
  g.use('files').write('x.txt', null)
}
export function down(g) {}
`

// A JS null argument crosses the bridge as the parameter's zero value, so the
// handle reports a meaningful type error instead of reflect's opaque
// "zero Value argument" panic.
func Test_Live_Stores_NullArgumentGetsClearError(t *testing.T) {
	run, migrationsDir, _, _ := setupStoresRun(t)
	writeMigration(t, migrationsDir, "20240101000000-null-arg.migration.ts", storesNullArg)

	pending, err := run.GetPending()
	if err != nil {
		t.Fatalf("GetPending() error = %v", err)
	}
	m := pending[0]
	m.AppliedAt = time.Now()

	err = run.ApplyMigration(m.Script.Up, []*migrationsmeta.MigrationMetadata{m.MigrationMetadata})
	if err == nil {
		t.Fatal("ApplyMigration() error = nil, want type error for null body")
	}
	if !strings.Contains(err.Error(), "expects a string or ArrayBuffer") {
		t.Errorf("error = %v, want the handle's type error, not a reflect panic", err)
	}
}

const storesEscape = `
export function up(g) {
  g.use('files').write('../escape.txt', 'x')
}
export function down(g) {}
`

func Test_Live_Stores_PathEscapeFailsMigration(t *testing.T) {
	run, migrationsDir, filesRoot, _ := setupStoresRun(t)
	writeMigration(t, migrationsDir, "20240101000000-escape.migration.ts", storesEscape)

	pending, err := run.GetPending()
	if err != nil {
		t.Fatalf("GetPending() error = %v", err)
	}
	m := pending[0]
	m.AppliedAt = time.Now()

	err = run.ApplyMigration(m.Script.Up, []*migrationsmeta.MigrationMetadata{m.MigrationMetadata})
	if err == nil {
		t.Fatal("ApplyMigration() error = nil, want path-escape error")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error = %v, want a path-escape message", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(filesRoot), "escape.txt")); statErr == nil {
		t.Error("write escaped the configured root")
	}
}
