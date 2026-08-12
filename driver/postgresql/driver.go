package postgresql

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"text/template"

	"github.com/telemetryos/graviton/config"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"github.com/dop251/goja"
	_ "github.com/lib/pq"
)

const MIGRATIONS_TABLE = "graviton_migrations"

//go:embed sql/create_migrations_table.sql
var createMigrationsTableSQL string

//go:embed sql/get_migrations.sql
var getMigrationsSQL string

//go:embed sql/delete_all_migrations.sql
var deleteAllMigrationsSQL string

//go:embed sql/insert_migration.sql
var insertMigrationSQL string

// sqlExecutor is satisfied by both *sql.DB and *sql.Tx, letting operations run
// either directly or inside this driver's open transaction.
type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type driverRuntimeData struct {
	sqlQueryCtorVal   goja.Value
	sqlTagFunctionVal goja.Value
}

type Driver struct {
	config      *config.DatabaseConfig
	db          *sql.DB
	runtimeData map[*goja.Runtime]*driverRuntimeData

	// tx is the single transaction that may be open on this driver at a time.
	tx *sql.Tx
}

func New(conf *config.DatabaseConfig) *Driver {
	return &Driver{
		config:      conf,
		runtimeData: make(map[*goja.Runtime]*driverRuntimeData),
	}
}

func (d *Driver) Connect(ctx context.Context) error {
	db, err := sql.Open("postgres", d.config.ConnectionUrl)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	d.db = db

	createSQL, err := d.renderSQL(createMigrationsTableSQL)
	if err != nil {
		return fmt.Errorf("failed to render create table SQL: %w", err)
	}

	if _, err := d.db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	createLockSQL, err := d.renderSQL(createLockTableSQL)
	if err != nil {
		return fmt.Errorf("failed to render create lock table SQL: %w", err)
	}

	if _, err := d.db.ExecContext(ctx, createLockSQL); err != nil {
		return fmt.Errorf("failed to create migrations lock table: %w", err)
	}

	return nil
}

func (d *Driver) Disconnect(ctx context.Context) error {
	if d.db == nil {
		return nil
	}
	return d.db.Close()
}

func (d *Driver) GetAppliedMigrationsMetadata(ctx context.Context) ([]*migrationsmeta.MigrationMetadata, error) {
	query, err := d.renderSQL(getMigrationsSQL)
	if err != nil {
		return nil, err
	}

	rows, err := d.executor().QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var migrations []*migrationsmeta.MigrationMetadata
	for rows.Next() {
		var m migrationsmeta.MigrationMetadata
		if err := rows.Scan(&m.Filename, &m.Source, &m.AppliedAt); err != nil {
			return nil, err
		}
		migrations = append(migrations, &m)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return migrations, nil
}

func (d *Driver) SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []*migrationsmeta.MigrationMetadata) error {
	execer := d.executor()

	deleteSQL, err := d.renderSQL(deleteAllMigrationsSQL)
	if err != nil {
		return err
	}

	if _, err := execer.ExecContext(ctx, deleteSQL); err != nil {
		return err
	}

	if len(migrationsMetadata) == 0 {
		return nil
	}

	insertSQL, err := d.renderSQL(insertMigrationSQL)
	if err != nil {
		return err
	}

	for _, m := range migrationsMetadata {
		if _, err := execer.ExecContext(ctx, insertSQL, m.Filename, m.Source, m.AppliedAt); err != nil {
			return err
		}
	}

	return nil
}

// BeginTx opens a transaction if none is open. It is idempotent.
func (d *Driver) BeginTx(ctx context.Context) error {
	_, err := d.ensureTx(ctx)
	return err
}

// ensureTx returns this driver's open transaction, beginning one if none is
// open yet. It backs both BeginTx and the JS-facing handle operations.
func (d *Driver) ensureTx(ctx context.Context) (*sql.Tx, error) {
	if d.tx != nil {
		return d.tx, nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	d.tx = tx
	return tx, nil
}

func (d *Driver) CommitTx(ctx context.Context) error {
	if d.tx == nil {
		return nil
	}
	err := d.tx.Commit()
	d.tx = nil
	return err
}

func (d *Driver) RollbackTx(ctx context.Context) error {
	if d.tx == nil {
		return nil
	}
	err := d.tx.Rollback()
	d.tx = nil
	return err
}

func (d *Driver) HasOpenTx() bool {
	return d.tx != nil
}

// executor returns the open transaction when one is active, else the raw
// connection pool. Tracking reads/writes use it without implicitly beginning a
// transaction; the JS-facing handle begins one lazily via ensureTx.
func (d *Driver) executor() sqlExecutor {
	if d.tx != nil {
		return d.tx
	}
	return d.db
}

func (d *Driver) Handle(ctx context.Context) any {
	return &Handle{ctx: ctx, driver: d}
}

func (d *Driver) Init(ctx context.Context, runtime *goja.Runtime) {
	d.runtimeData[runtime] = &driverRuntimeData{
		sqlQueryCtorVal:   runtime.ToValue(SQLQueryCtor),
		sqlTagFunctionVal: runtime.ToValue(createSQLTagFunction(d)),
	}
}

func (d *Driver) Globals(ctx context.Context, runtime *goja.Runtime) map[string]any {
	rtData := d.runtimeData[runtime]
	globals := map[string]any{}
	globals["SQLQuery"] = rtData.sqlQueryCtorVal
	globals["sql"] = rtData.sqlTagFunctionVal
	return globals
}

func (d *Driver) MaybeFromJSValue(ctx context.Context, runtime *goja.Runtime, value goja.Value) (any, bool) {
	rtData := d.runtimeData[runtime]
	if rtData == nil {
		return nil, false
	}
	if IsSQLQuery(runtime, value, rtData.sqlQueryCtorVal) {
		return SQLQueryFromJSValue(runtime, value), true
	}
	return nil, false
}

func (d *Driver) renderSQL(sqlTemplate string) (string, error) {
	tmpl, err := template.New("sql").Parse(sqlTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	data := map[string]string{
		"TableName":     MIGRATIONS_TABLE,
		"LockTableName": MIGRATIONS_LOCK_TABLE,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
