package migrations

import (
	"context"
	"github.com/telemetryos/graviton/config"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiveAsyncTransactions(t *testing.T) {
	cases := []struct {
		name, body, failure string
		count               int
	}{
		{"commit", `await a.withTransaction(async a => { await Promise.resolve(); await a.collection('items').insertOne({n: 1}); return 42 }).then(value => { if (value !== 42) throw Error('result lost') })`, "", 1},
		{"independent databases", `try { await a.withTransaction(async a => { a.collection('items').insertOne({n: 1}); await db.use('b').withTransaction(async b => { b.collection('items').insertOne({n: 2}) }); throw Error('abort a') }) } catch (_) {} if (db.use('b').collection('items').find({}).length !== 1) throw Error('lost independent commit')`, "", 0},
		{"concurrent same database", `await Promise.all([a.withTransaction(async a => { await Promise.resolve(); a.collection('items').insertOne({n: 1}) }), a.withTransaction(async a => {})])`, "already has an active", 1},
		{"rollback", `await a.withTransaction(async a => { await a.collection('items').insertOne({n: 1}); await Promise.resolve(); throw Error('reject callback') })`, "reject callback", 0},
		{"caught rejection", `try { await a.withTransaction(async a => { a.collection('items').insertOne({n: 1}); throw Error('rollback') }) } catch (_) {} await a.withTransaction(async a => { a.collection('items').insertOne({n: 2}) })`, "", 1},
		{"independent handle", `await a.withTransaction(async transaction => { a.collection('items').insertOne({n: 1}); transaction.collection('items').insertOne({n: 2}); throw Error('rollback') })`, "rollback", 1},
		{"expired collection", `let items; await a.withTransaction(async a => { items = a.collection('items'); items.insertOne({n: 1}) }); items.insertOne({n: 2})`, "no longer active", 1},
		{"nested", `await a.withTransaction(async a => { a.collection('items').insertOne({n: 1}); await a.withTransaction(async a => {}) })`, "already has an active", 0},
		{"pending", `await a.withTransaction(async a => { a.collection('items').insertOne({n: 1}); await new Promise(() => {}) })`, "unresolved Promise", 0},
		{"unhandled", `a.withTransaction(async a => { a.collection('items').insertOne({n: 1}); throw Error('unobserved') })`, "unobserved", 0},
		{"native error catch", `await a.withTransaction(async a => { a.collection('items').insertOne({_id: 1}); a.collection('items').insertOne({_id: 1}) })`, "duplicate key", 0},
		{"async required", `await a.withTransaction(a => { a.collection('items').insertOne({n: 1}) })`, "must return a Promise", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run, dir, database, _ := setupTwoDbRun(t)
			// Pre-create the collection so independent writes do not contend on creation.
			rawClient(t).Database(database).CreateCollection(run.ctx, "items")
			writeMigration(t, dir, "20240101000000-transaction.migration.ts", "export async function up(db) { const a = db.use('a'); "+tc.body+" } export async function down(db) {}")
			pending, err := run.GetPending()
			if err != nil {
				t.Fatal(err)
			}
			err = run.ApplyMigration(pending[0].Script.Up, nil)
			if tc.failure == "" && err != nil {
				t.Fatal(err)
			}
			if tc.failure != "" && (err == nil || !strings.Contains(err.Error(), tc.failure)) {
				t.Fatalf("error = %v, want %q", err, tc.failure)
			}
			if got := countDocs(t, database, "items"); got != int64(tc.count) {
				t.Fatalf("count = %d, want %d", got, tc.count)
			}
		})
	}
}

func TestSQLiteAsyncTransactions(t *testing.T) {
	conf := &config.Config{MigrationsDb: "database", Databases: []*config.DatabaseConfig{
		{Name: "database", Kind: config.DatabaseKindSQLite, ConnectionUrl: "file:" + filepath.Join(t.TempDir(), "test.db")},
	}}
	run := NewRun(context.Background(), conf)
	if err := run.Connect(); err != nil {
		t.Fatal(err)
	}
	defer run.Disconnect()
	script := run.newScript("var migration = { async up(db) { "+
		"db.exec(sql`CREATE TABLE items (id INTEGER PRIMARY KEY)`);"+
		"await db.withTransaction(async db => { await Promise.resolve(); db.exec(sql`INSERT INTO items VALUES (1)`); });"+
		"try { await db.withTransaction(async db => { db.exec(sql`INSERT INTO items VALUES (2)`); await Promise.reject(Error('rollback')); }); } catch (_) {}"+
		"const row = db.queryOne(sql`SELECT COUNT(*) AS count FROM items`); if (row.count !== 1) throw Error('incorrect transaction result');"+
		"}}", "sqlite-transactions")
	if err := run.ApplyMigration(script.Up, nil); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsTransaction(t *testing.T) {
	conf := &config.Config{MigrationsDb: "files", Databases: []*config.DatabaseConfig{
		{Name: "files", Kind: config.DatabaseKindFS, ConnectionUrl: t.TempDir()},
	}}
	run := NewRun(context.Background(), conf)
	if err := run.Connect(); err != nil {
		t.Fatal(err)
	}
	defer run.Disconnect()
	script := run.newScript("var migration = {async up(db) { await db.withTransaction(async db => { throw Error('callback must not run') }) }}", "unsupported-transaction")
	err := run.ApplyMigration(script.Up, nil)
	if err == nil || !strings.Contains(err.Error(), "does not support transactions") {
		t.Fatalf("error = %v", err)
	}
}

func TestMongoRunCommandIndexes(t *testing.T) {
	run, _, _, _ := setupTwoDbRun(t)
	script := run.newScript(`var migration = {async up(db) {
  const database = db.use('b');
  database.runCommand('createIndexes', 'settings', {indexes:[{key:{accountId:1},name:'accountId_1',unique:true}]});
  const indexes = database.runCommand('listIndexes','settings').cursor.firstBatch;
  if (!indexes.some(index => index.name === 'accountId_1' && index.unique)) throw Error('index was not created');
  await database.withTransaction(async database => {
    database.runCommand('insert','settings',{documents:[{accountId:'fixture'}]});
  });
  try {
    await database.withTransaction(async database => {
      database.runCommand('insert','settings',{documents:[{accountId:'rollback'}]});
      throw Error('rollback');
    });
  } catch (_) {}
  if (database.collection('settings').find({}).length !== 1) throw Error('command did not use the transaction');
  database.runCommand('dropIndexes','settings',{index:'accountId_1'});
  if (database.runCommand('listIndexes','settings').cursor.firstBatch.some(index => index.name === 'accountId_1')) throw Error('index was not dropped');
 }} `, "mongo-commands")
	if err := run.ApplyMigration(script.Up, nil); err != nil {
		t.Fatal(err)
	}
}
