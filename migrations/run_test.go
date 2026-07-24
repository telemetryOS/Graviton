package migrations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/telemetryos/graviton/config"
	"github.com/telemetryos/graviton/driver"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// -----------------------------------------------------------------------------
// Fake-driver tests: deterministic proof of the commit ordering (data
// transactions first, marker last, in its own transaction) with no database.
// -----------------------------------------------------------------------------

type fakeDriver struct {
	name            string
	log             *[]string
	commitFail      bool
	inTx            bool
	applied         []*migrationsmeta.MigrationMetadata
	setAppliedCalls int
}

func (f *fakeDriver) record(event string) { *f.log = append(*f.log, event+":"+f.name) }

func (f *fakeDriver) Connect(ctx context.Context) error    { return nil }
func (f *fakeDriver) Disconnect(ctx context.Context) error { return nil }

func (f *fakeDriver) GetAppliedMigrationsMetadata(ctx context.Context) ([]*migrationsmeta.MigrationMetadata, error) {
	return f.applied, nil
}

func (f *fakeDriver) SetAppliedMigrationsMetadata(ctx context.Context, m []*migrationsmeta.MigrationMetadata) error {
	f.setAppliedCalls++
	f.applied = m
	f.record("set")
	return nil
}

func (f *fakeDriver) BeginTx(ctx context.Context) error {
	if f.inTx {
		return nil
	}
	f.inTx = true
	f.record("begin")
	return nil
}

func (f *fakeDriver) CommitTx(ctx context.Context) error {
	if !f.inTx {
		return nil
	}
	f.inTx = false
	if f.commitFail {
		f.record("commitfail")
		return fmt.Errorf("commit failed for %s", f.name)
	}
	f.record("commit")
	return nil
}

func (f *fakeDriver) RollbackTx(ctx context.Context) error {
	if !f.inTx {
		return nil
	}
	f.inTx = false
	f.record("rollback")
	return nil
}

func (f *fakeDriver) HasOpenTx() bool                                 { return f.inTx }
func (f *fakeDriver) Handle(ctx context.Context) any                  { return struct{}{} }
func (f *fakeDriver) Init(ctx context.Context, runtime *goja.Runtime) {}
func (f *fakeDriver) Globals(ctx context.Context, r *goja.Runtime) map[string]any {
	return nil
}
func (f *fakeDriver) MaybeFromJSValue(ctx context.Context, r *goja.Runtime, v goja.Value) (any, bool) {
	return nil, false
}
func (f *fakeDriver) MaybeIntoJSValue(ctx context.Context, r *goja.Runtime, v any) (goja.Value, bool) {
	return nil, false
}

// fakeRun builds a Run over fake drivers a, b and a dedicated tracking driver t.
func fakeRun() (*Run, *fakeDriver, *fakeDriver, *fakeDriver, *[]string) {
	log := &[]string{}
	a := &fakeDriver{name: "a", log: log}
	b := &fakeDriver{name: "b", log: log}
	tracking := &fakeDriver{name: "t", log: log}
	run := &Run{
		ctx:  context.Background(),
		conf: &config.Config{MigrationsDb: "t"},
		drivers: map[string]driver.Driver{
			"a": a,
			"b": b,
			"t": tracking,
		},
		order: []string{"a", "b", "t"},
	}
	return run, a, b, tracking, log
}

func indexOf(log []string, event string) int {
	for i, e := range log {
		if e == event {
			return i
		}
	}
	return -1
}

func Test_ApplyMigration_MarkerWrittenLast(t *testing.T) {
	run, a, b, tracking, log := fakeRun()

	// Body interleaves writes across a and b (t is untouched by the body).
	body := func() error {
		a.BeginTx(run.ctx)
		b.BeginTx(run.ctx)
		a.BeginTx(run.ctx) // idempotent second touch
		return nil
	}

	markerList := []*migrationsmeta.MigrationMetadata{{Filename: "20240101000000-x.migration.ts"}}
	if err := run.ApplyMigration(body, markerList); err != nil {
		t.Fatalf("ApplyMigration() error = %v, want nil", err)
	}

	setIdx := indexOf(*log, "set:t")
	if setIdx == -1 {
		t.Fatalf("tracking marker was never written; log = %v", *log)
	}
	if got := indexOf(*log, "commit:a"); got == -1 || got > setIdx {
		t.Errorf("data commit for a (%d) must precede marker write (%d); log = %v", got, setIdx, *log)
	}
	if got := indexOf(*log, "commit:b"); got == -1 || got > setIdx {
		t.Errorf("data commit for b (%d) must precede marker write (%d); log = %v", got, setIdx, *log)
	}
	if beginIdx := indexOf(*log, "begin:t"); beginIdx == -1 || beginIdx > setIdx {
		t.Errorf("marker write must run in its own transaction begun before the set; log = %v", *log)
	}
	if tracking.setAppliedCalls != 1 {
		t.Errorf("tracking SetAppliedMigrationsMetadata calls = %d, want 1", tracking.setAppliedCalls)
	}
	if len(tracking.applied) != 1 {
		t.Errorf("persisted marker list len = %d, want 1", len(tracking.applied))
	}
}

func Test_ApplyMigration_DataCommitFailure_MarkerNotWritten(t *testing.T) {
	run, a, b, tracking, log := fakeRun()
	b.commitFail = true

	body := func() error {
		a.BeginTx(run.ctx)
		b.BeginTx(run.ctx)
		return nil
	}

	err := run.ApplyMigration(body, []*migrationsmeta.MigrationMetadata{{Filename: "20240101000000-x.migration.ts"}})
	if err == nil {
		t.Fatal("ApplyMigration() error = nil, want error from failed data commit")
	}

	// a committed before b failed; a stays committed, the marker is not written.
	if indexOf(*log, "commit:a") == -1 {
		t.Errorf("database a should have committed before the failure; log = %v", *log)
	}
	if indexOf(*log, "commitfail:b") == -1 {
		t.Errorf("database b commit should have failed; log = %v", *log)
	}
	if tracking.setAppliedCalls != 0 {
		t.Errorf("tracking marker was written despite a data commit failure; calls = %d", tracking.setAppliedCalls)
	}
	if indexOf(*log, "set:t") != -1 {
		t.Errorf("marker must not be written when a data commit fails; log = %v", *log)
	}
}

func Test_ApplyMigration_BodyError_RollsBackAllNoMarker(t *testing.T) {
	run, a, b, tracking, log := fakeRun()

	body := func() error {
		a.BeginTx(run.ctx)
		b.BeginTx(run.ctx)
		return errors.New("body failed")
	}

	err := run.ApplyMigration(body, []*migrationsmeta.MigrationMetadata{{Filename: "20240101000000-x.migration.ts"}})
	if err == nil {
		t.Fatal("ApplyMigration() error = nil, want error from body")
	}
	if indexOf(*log, "rollback:a") == -1 || indexOf(*log, "rollback:b") == -1 {
		t.Errorf("both open transactions should have rolled back; log = %v", *log)
	}
	if indexOf(*log, "commit:a") != -1 || indexOf(*log, "commit:b") != -1 {
		t.Errorf("no data transaction should commit when the body fails; log = %v", *log)
	}
	if tracking.setAppliedCalls != 0 {
		t.Errorf("marker must not be written when the body fails; calls = %d", tracking.setAppliedCalls)
	}
}

func Test_ApplyMigration_BodyPanic_Recovered(t *testing.T) {
	run, a, _, tracking, log := fakeRun()

	body := func() error {
		a.BeginTx(run.ctx)
		panic("boom")
	}

	err := run.ApplyMigration(body, []*migrationsmeta.MigrationMetadata{{Filename: "20240101000000-x.migration.ts"}})
	if err == nil {
		t.Fatal("ApplyMigration() error = nil, want error recovered from panic")
	}
	if indexOf(*log, "rollback:a") == -1 {
		t.Errorf("open transaction should have rolled back after panic; log = %v", *log)
	}
	if tracking.setAppliedCalls != 0 {
		t.Errorf("marker must not be written after a panic; calls = %d", tracking.setAppliedCalls)
	}
}

// -----------------------------------------------------------------------------
// Root-handle / use() contract tests (no database connection required).
// -----------------------------------------------------------------------------

func mongoCfg(name string) *config.DatabaseConfig {
	return &config.DatabaseConfig{
		Name:          name,
		Kind:          config.DatabaseKindMongoDB,
		ConnectionUrl: testMongoURL,
		DatabaseName:  "graviton_run_test_" + name,
	}
}

func Test_RootHandle_SingleDatabaseBindsDirectly(t *testing.T) {
	conf := &config.Config{
		MigrationsDb:   "only",
		MigrationsPath: "migrations",
		Databases:      []*config.DatabaseConfig{mongoCfg("only")},
	}
	run := NewRun(context.Background(), conf)

	h := run.rootHandle()
	if h.drv == nil {
		t.Error("single-database root handle is unbound; it should bind directly to the only database")
	}
	if h.alias != "only" {
		t.Errorf("root handle alias = %q, want %q", h.alias, "only")
	}
	// use() still works on a single-database project and returns the same type.
	if bound := h.Use("only"); bound.drv == nil {
		t.Error("use() on the single database returned an unbound handle")
	}
}

func Test_RootHandle_MultiDatabaseUnboundRequiresUse(t *testing.T) {
	conf := &config.Config{
		MigrationsDb:   "a",
		MigrationsPath: "migrations",
		Databases:      []*config.DatabaseConfig{mongoCfg("a"), mongoCfg("b")},
	}
	run := NewRun(context.Background(), conf)

	h := run.rootHandle()
	if h.drv != nil {
		t.Fatalf("multi-database root handle is bound to %q; it should be unbound until use()", h.alias)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("collection() on the unbound root did not panic, want a clear multi-database error")
		}
		err, _ := r.(error)
		if err == nil || !strings.Contains(err.Error(), "use(alias)") {
			t.Errorf("unbound-handle panic = %v, want mention of use(alias)", r)
		}
	}()
	h.Collection("x")
}

func Test_Use_ReturnsDistinctBoundHandles(t *testing.T) {
	conf := &config.Config{
		MigrationsDb:   "a",
		MigrationsPath: "migrations",
		Databases:      []*config.DatabaseConfig{mongoCfg("a"), mongoCfg("b")},
	}
	run := NewRun(context.Background(), conf)

	root := run.rootHandle()
	h1 := root.Use("b")
	h2 := root.Use("b")

	if h1 == h2 {
		t.Error("use() returned the same handle instance twice; each call must return a new bound instance")
	}
	if h1.drv == nil || h2.drv == nil {
		t.Fatal("use() returned an unbound handle")
	}
	if h1.drv != h2.drv {
		t.Error("use() of the same alias should bind to the same underlying driver")
	}
	if h1.alias != "b" {
		t.Errorf("bound handle alias = %q, want %q", h1.alias, "b")
	}
}

func Test_Use_UnknownAliasPanicsListingAliases(t *testing.T) {
	conf := &config.Config{
		MigrationsDb:   "a",
		MigrationsPath: "migrations",
		Databases:      []*config.DatabaseConfig{mongoCfg("a"), mongoCfg("b")},
	}
	run := NewRun(context.Background(), conf)
	root := run.rootHandle()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("use(unknown) did not panic")
		}
		err, _ := r.(error)
		if err == nil || !strings.Contains(err.Error(), "bogus") || !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
			t.Errorf("use(unknown) panic = %v, want the alias and configured aliases", r)
		}
	}()
	root.Use("bogus")
}

// -----------------------------------------------------------------------------
// rename() contract tests (no database connection required).
// -----------------------------------------------------------------------------

func Test_Rename_UnboundRootPanicsRequiringUse(t *testing.T) {
	conf := &config.Config{
		MigrationsDb:   "a",
		MigrationsPath: "migrations",
		Databases:      []*config.DatabaseConfig{mongoCfg("a"), mongoCfg("b")},
	}
	run := NewRun(context.Background(), conf)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("rename() on the unbound root did not panic, want a multi-database error")
		}
		err, _ := r.(error)
		if err == nil || !strings.Contains(err.Error(), "use(alias)") {
			t.Errorf("unbound-handle rename panic = %v, want mention of use(alias)", r)
		}
	}()
	run.rootHandle().Rename("whatever")
}

func Test_Rename_MigrationsDbPanics(t *testing.T) {
	conf := &config.Config{
		MigrationsDb:   "a",
		MigrationsPath: "migrations",
		Databases:      []*config.DatabaseConfig{mongoCfg("a"), mongoCfg("b")},
	}
	run := NewRun(context.Background(), conf)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("rename() on the migrations_db did not panic")
		}
		err, _ := r.(error)
		if err == nil || !strings.Contains(err.Error(), "migrations_db") {
			t.Errorf("migrations_db rename panic = %v, want mention of migrations_db", r)
		}
	}()
	run.rootHandle().Use("a").Rename("a__migrated__")
}

func Test_Rename_UnknownAliasPanics(t *testing.T) {
	conf := &config.Config{
		MigrationsDb:   "a",
		MigrationsPath: "migrations",
		Databases:      []*config.DatabaseConfig{mongoCfg("a"), mongoCfg("b")},
	}
	run := NewRun(context.Background(), conf)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("use(unknown).rename() did not panic")
		}
		err, _ := r.(error)
		if err == nil || !strings.Contains(err.Error(), "bogus") {
			t.Errorf("unknown-alias rename panic = %v, want mention of the alias", r)
		}
	}()
	run.rootHandle().Use("bogus").Rename("bogus__migrated__")
}

func Test_Rename_UnsupportedKindPanics(t *testing.T) {
	run, _, _, _, _ := fakeRun()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("rename() on a non-mongodb database did not panic")
		}
		err, _ := r.(error)
		if err == nil || !strings.Contains(err.Error(), "mongodb") {
			t.Errorf("unsupported-kind rename panic = %v, want mention of mongodb", r)
		}
	}()
	run.rootHandle().Use("a").Rename("a__migrated__")
}

// -----------------------------------------------------------------------------
// Live two-database Mongo tests: interleaving, rollback, and linear ordering.
// -----------------------------------------------------------------------------

const testMongoURL = "mongodb://localhost:27017"

func setupTwoDbRun(t *testing.T) (*Run, string, string, string) {
	t.Helper()

	projectDir := t.TempDir()
	migrationsDir := filepath.Join(projectDir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0755); err != nil {
		t.Fatalf("mkdir migrations: %v", err)
	}

	dbA := "graviton_run_test_a"
	dbB := "graviton_run_test_b"

	conf := &config.Config{
		ProjectPath:    projectDir,
		MigrationsDb:   "a",
		MigrationsPath: "migrations",
		Databases: []*config.DatabaseConfig{
			{Name: "a", Kind: config.DatabaseKindMongoDB, ConnectionUrl: testMongoURL, DatabaseName: dbA},
			{Name: "b", Kind: config.DatabaseKindMongoDB, ConnectionUrl: testMongoURL, DatabaseName: dbB},
		},
	}

	run := NewRun(context.Background(), conf)
	if err := run.Connect(); err != nil {
		t.Skipf("MongoDB not available on localhost: %v", err)
	}

	dropDatabases(t, dbA, dbB)
	t.Cleanup(func() {
		dropDatabases(t, dbA, dbB)
		run.Disconnect()
	})

	return run, migrationsDir, dbA, dbB
}

func rawClient(t *testing.T) *mongo.Client {
	t.Helper()
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(testMongoURL))
	if err != nil {
		t.Fatalf("raw mongo connect: %v", err)
	}
	t.Cleanup(func() { client.Disconnect(context.Background()) })
	return client
}

func dropDatabases(t *testing.T, dbs ...string) {
	t.Helper()
	client := rawClient(t)
	for _, db := range dbs {
		if err := client.Database(db).Drop(context.Background()); err != nil {
			t.Fatalf("drop database %s: %v", db, err)
		}
	}
}

func countDocs(t *testing.T, dbName, coll string) int64 {
	t.Helper()
	client := rawClient(t)
	n, err := client.Database(dbName).Collection(coll).CountDocuments(context.Background(), bson.M{})
	if err != nil {
		t.Fatalf("count %s.%s: %v", dbName, coll, err)
	}
	return n
}

func writeMigration(t *testing.T, dir, filename, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(src), 0644); err != nil {
		t.Fatalf("write migration %s: %v", filename, err)
	}
}

const interleaveUp = `
export function up(g) {
  g.use('a').collection('items').insertOne({ n: 1 })
  g.use('b').collection('items').insertOne({ n: 2 })
  g.use('a').collection('items').insertOne({ n: 3 })
}
export function down(g) {
  g.use('a').collection('items').deleteMany({})
  g.use('b').collection('items').deleteMany({})
}
`

func Test_Live_InterleavedTwoDatabase_Commit(t *testing.T) {
	run, migrationsDir, dbA, dbB := setupTwoDbRun(t)
	writeMigration(t, migrationsDir, "20240101000000-interleave.migration.ts", interleaveUp)

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
		t.Fatalf("ApplyMigration() error = %v, want nil", err)
	}

	if got := countDocs(t, dbA, "items"); got != 2 {
		t.Errorf("db a items = %d, want 2 (interleaved writes to a committed)", got)
	}
	if got := countDocs(t, dbB, "items"); got != 1 {
		t.Errorf("db b items = %d, want 1 (interleaved write to b committed)", got)
	}

	applied, err := run.GetApplied()
	if err != nil {
		t.Fatalf("GetApplied() error = %v", err)
	}
	if len(applied) != 1 {
		t.Errorf("GetApplied() = %d, want 1 (marker written to migrations_db)", len(applied))
	}
}

const interleaveFail = `
export function up(g) {
  g.use('a').collection('items').insertOne({ n: 1 })
  g.use('b').collection('items').insertOne({ n: 2 })
  throw new Error('boom')
}
export function down(g) {}
`

func Test_Live_InterleavedTwoDatabase_FailureRollsBackBothNoMarker(t *testing.T) {
	run, migrationsDir, dbA, dbB := setupTwoDbRun(t)
	writeMigration(t, migrationsDir, "20240101000000-fail.migration.ts", interleaveFail)

	pending, err := run.GetPending()
	if err != nil {
		t.Fatalf("GetPending() error = %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("GetPending() returned %d migrations, want 1", len(pending))
	}

	m := pending[0]
	m.AppliedAt = time.Now()
	if err := run.ApplyMigration(m.Script.Up, []*migrationsmeta.MigrationMetadata{m.MigrationMetadata}); err == nil {
		t.Fatal("ApplyMigration() error = nil, want error from throwing migration")
	}

	if got := countDocs(t, dbA, "items"); got != 0 {
		t.Errorf("db a items = %d, want 0 (should roll back)", got)
	}
	if got := countDocs(t, dbB, "items"); got != 0 {
		t.Errorf("db b items = %d, want 0 (should roll back)", got)
	}

	applied, err := run.GetApplied()
	if err != nil {
		t.Fatalf("GetApplied() error = %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("GetApplied() = %d, want 0 (marker must be absent after failure)", len(applied))
	}
}

const renameWithOpenTx = `
export function up(g) {
  g.use('b').collection('items').insertOne({ n: 1 })
  g.use('b').rename('graviton_run_test_b__migrated__')
}
export function down(g) {}
`

func Test_Live_Rename_OpenTransactionOnSourceFails(t *testing.T) {
	run, migrationsDir, _, dbB := setupTwoDbRun(t)
	migratedB := dbB + "__migrated__"
	t.Cleanup(func() { dropDatabases(t, migratedB) })
	writeMigration(t, migrationsDir, "20240101000000-rename-open-tx.migration.ts", renameWithOpenTx)

	pending, err := run.GetPending()
	if err != nil {
		t.Fatalf("GetPending() error = %v", err)
	}
	m := pending[0]
	m.AppliedAt = time.Now()
	err = run.ApplyMigration(m.Script.Up, []*migrationsmeta.MigrationMetadata{m.MigrationMetadata})
	if err == nil {
		t.Fatal("ApplyMigration() error = nil, want error renaming a database with an open transaction")
	}
	if !strings.Contains(err.Error(), "transaction") {
		t.Errorf("error = %v, want it to mention the open transaction", err)
	}

	// The guard fired before any collection moved; the source is untouched (its
	// insert rolled back) and the __migrated__ database was never created.
	if got := countDocs(t, dbB, "items"); got != 0 {
		t.Errorf("db b items = %d, want 0 (insert should roll back)", got)
	}
	if got := countDocs(t, migratedB, "items"); got != 0 {
		t.Errorf("migrated db items = %d, want 0 (rename must not have run)", got)
	}
}

const orderOne = `
export function up(g) { g.use('a').collection('items').insertOne({ m: 'one' }) }
export function down(g) { g.use('a').collection('items').deleteMany({ m: 'one' }) }
`

const orderTwo = `
export function up(g) {
  g.use('a').collection('items').insertOne({ m: 'two' })
  g.use('b').collection('items').insertOne({ m: 'two' })
}
export function down(g) {
  g.use('a').collection('items').deleteMany({ m: 'two' })
  g.use('b').collection('items').deleteMany({ m: 'two' })
}
`

// applyAll mirrors the up command: cumulative marker list, one migration at a
// time.
func applyAll(t *testing.T, run *Run) {
	t.Helper()
	pending, err := run.GetPending()
	if err != nil {
		t.Fatalf("GetPending() error = %v", err)
	}
	appliedMeta, err := run.AppliedMetadata()
	if err != nil {
		t.Fatalf("AppliedMetadata() error = %v", err)
	}
	marker := append([]*migrationsmeta.MigrationMetadata{}, appliedMeta...)
	for _, m := range pending {
		m.AppliedAt = time.Now()
		marker = append(marker, m.MigrationMetadata)
		if err := run.ApplyMigration(m.Script.Up, marker); err != nil {
			t.Fatalf("ApplyMigration(%s) error = %v", m.Filename, err)
		}
	}
}

func migrationNames(ms []*Migration) []string {
	names := make([]string, 0, len(ms))
	for _, m := range ms {
		names = append(names, m.Name())
	}
	return names
}

func Test_Live_LinearOrdering_UpDownStatusSetHead(t *testing.T) {
	run, migrationsDir, dbA, dbB := setupTwoDbRun(t)
	writeMigration(t, migrationsDir, "20240101000000-one.migration.ts", orderOne)
	writeMigration(t, migrationsDir, "20240101000001-two.migration.ts", orderTwo)

	// Pending is the linear set in filename order.
	pending, err := run.GetPending()
	if err != nil {
		t.Fatalf("GetPending() error = %v", err)
	}
	if got := migrationNames(pending); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("pending order = %v, want [one two]", got)
	}

	// up: apply both.
	applyAll(t, run)
	applied, _ := run.GetApplied()
	if got := migrationNames(applied); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("applied after up = %v, want [one two]", got)
	}
	if countDocs(t, dbA, "items") != 2 || countDocs(t, dbB, "items") != 1 {
		t.Fatalf("doc counts after up: a=%d b=%d, want a=2 b=1", countDocs(t, dbA, "items"), countDocs(t, dbB, "items"))
	}

	// down to two: rolls back only two (across both databases).
	rollback, _ := run.GetApplied()
	// most-recent first
	two := rollback[1]
	if two.Name() != "two" {
		t.Fatalf("expected second applied to be two, got %s", two.Name())
	}
	remaining := []*migrationsmeta.MigrationMetadata{rollback[0].MigrationMetadata}
	if err := run.ApplyMigration(two.Script.Down, remaining); err != nil {
		t.Fatalf("down(two) error = %v", err)
	}
	applied, _ = run.GetApplied()
	if got := migrationNames(applied); len(got) != 1 || got[0] != "one" {
		t.Fatalf("applied after down = %v, want [one]", got)
	}
	if countDocs(t, dbA, "items") != 1 || countDocs(t, dbB, "items") != 0 {
		t.Fatalf("doc counts after down: a=%d b=%d, want a=1 b=0", countDocs(t, dbA, "items"), countDocs(t, dbB, "items"))
	}

	// set-head to two: records both as applied without running them (b stays
	// empty because the body is not executed).
	all := append([]*Migration{}, applied...)
	pendingAfter, _ := run.GetPending()
	all = append(all, pendingAfter...)
	head := []*migrationsmeta.MigrationMetadata{}
	for _, m := range all {
		head = append(head, &migrationsmeta.MigrationMetadata{Filename: m.Filename, Source: m.Source, AppliedAt: time.Now()})
		if m.Name() == "two" {
			break
		}
	}
	if err := run.SetHead(head); err != nil {
		t.Fatalf("SetHead() error = %v", err)
	}
	applied, _ = run.GetApplied()
	if got := migrationNames(applied); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("applied after set-head = %v, want [one two]", got)
	}
	if countDocs(t, dbB, "items") != 0 {
		t.Errorf("set-head should not run bodies; db b items = %d, want 0", countDocs(t, dbB, "items"))
	}

	// status: nothing pending now.
	pendingAfter, _ = run.GetPending()
	if len(pendingAfter) != 0 {
		t.Errorf("pending after set-head = %v, want none", migrationNames(pendingAfter))
	}
}

// -----------------------------------------------------------------------------
// Live rename round-trip: a retire-databases migration whose up() renames a
// database to a __migrated__ name and whose down() renames it back.
// -----------------------------------------------------------------------------

const retireUpDown = `
export function up(g) {
  g.use('live').rename('graviton_rt_live__migrated__')
}
export function down(g) {
  g.use('retired').rename('graviton_rt_live')
}
`

func Test_Live_Rename_RetireRoundTrip(t *testing.T) {
	projectDir := t.TempDir()
	migrationsDir := filepath.Join(projectDir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0755); err != nil {
		t.Fatalf("mkdir migrations: %v", err)
	}

	const (
		tracking = "graviton_rt_tracking"
		live     = "graviton_rt_live"
		migrated = "graviton_rt_live__migrated__"
	)

	conf := &config.Config{
		ProjectPath:    projectDir,
		MigrationsDb:   "tracking",
		MigrationsPath: "migrations",
		Databases: []*config.DatabaseConfig{
			{Name: "tracking", Kind: config.DatabaseKindMongoDB, ConnectionUrl: testMongoURL, DatabaseName: tracking},
			{Name: "live", Kind: config.DatabaseKindMongoDB, ConnectionUrl: testMongoURL, DatabaseName: live},
			// The retired alias reaches the __migrated__ database so down() can
			// rename it back — rename addresses its source by config alias.
			{Name: "retired", Kind: config.DatabaseKindMongoDB, ConnectionUrl: testMongoURL, DatabaseName: migrated},
		},
	}

	run := NewRun(context.Background(), conf)
	if err := run.Connect(); err != nil {
		t.Skipf("MongoDB not available on localhost: %v", err)
	}
	dropDatabases(t, tracking, live, migrated)
	t.Cleanup(func() {
		dropDatabases(t, tracking, live, migrated)
		run.Disconnect()
	})

	// Seed the live database before retiring it.
	seed := rawClient(t).Database(live).Collection("items")
	if _, err := seed.InsertOne(context.Background(), bson.M{"n": 1}); err != nil {
		t.Fatalf("seed live db: %v", err)
	}

	writeMigration(t, migrationsDir, "20240101000000-retire-live.migration.ts", retireUpDown)

	pending, err := run.GetPending()
	if err != nil {
		t.Fatalf("GetPending() error = %v", err)
	}
	m := pending[0]
	m.AppliedAt = time.Now()

	// up: live is retired to the __migrated__ name and dropped.
	if err := run.ApplyMigration(m.Script.Up, []*migrationsmeta.MigrationMetadata{m.MigrationMetadata}); err != nil {
		t.Fatalf("ApplyMigration(up) error = %v", err)
	}
	if got := countDocs(t, live, "items"); got != 0 {
		t.Errorf("live items after retire = %d, want 0 (source dropped)", got)
	}
	if got := countDocs(t, migrated, "items"); got != 1 {
		t.Errorf("migrated items after retire = %d, want 1 (collection moved)", got)
	}
	if applied, _ := run.GetApplied(); len(applied) != 1 {
		t.Errorf("applied after up = %d, want 1 (marker written despite non-transactional rename)", len(applied))
	}

	// down: renamed back to the live name.
	if err := run.ApplyMigration(m.Script.Down, []*migrationsmeta.MigrationMetadata{}); err != nil {
		t.Fatalf("ApplyMigration(down) error = %v", err)
	}
	if got := countDocs(t, migrated, "items"); got != 0 {
		t.Errorf("migrated items after rename-back = %d, want 0 (source dropped)", got)
	}
	if got := countDocs(t, live, "items"); got != 1 {
		t.Errorf("live items after rename-back = %d, want 1 (round-trip)", got)
	}
}
