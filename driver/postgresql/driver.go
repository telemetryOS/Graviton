package postgresql

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"text/template"

	"graviton/config"
	migrationsmeta "graviton/migrations-meta"

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

type contextKey string

const txContextKey contextKey = "postgresql_tx"

type Driver struct {
	config           *config.DatabaseConfig
	db               *sql.DB
	sqlQueryCtorVal  goja.Value
	sqlTagFunctionVal goja.Value
}

func New(conf *config.DatabaseConfig) *Driver {
	return &Driver{config: conf}
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

	rows, err := d.db.QueryContext(ctx, query)
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
	var execer interface {
		ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	}

	if tx := d.getTxFromContext(ctx); tx != nil {
		execer = tx
	} else {
		execer = d.db
	}

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

func (d *Driver) WithTransaction(ctx context.Context, fn func(context.Context) error) (returnErr error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			if e, ok := r.(error); ok {
				returnErr = e
			} else {
				returnErr = fmt.Errorf("panic in transaction: %v", r)
			}
		} else if returnErr != nil {
			tx.Rollback()
		} else {
			returnErr = tx.Commit()
		}
	}()

	txCtx := context.WithValue(ctx, txContextKey, tx)

	return fn(txCtx)
}

func (d *Driver) Handle(ctx context.Context) any {
	return &Handle{ctx: ctx, driver: d}
}

func (d *Driver) Init(ctx context.Context, runtime *goja.Runtime) {
	d.sqlQueryCtorVal = runtime.ToValue(SQLQueryCtor)
	d.sqlTagFunctionVal = runtime.ToValue(createSQLTagFunction(d))
}

func (d *Driver) Globals(ctx context.Context, runtime *goja.Runtime) map[string]any {
	globals := map[string]any{}
	globals["SQLQuery"] = d.sqlQueryCtorVal
	globals["sql"] = d.sqlTagFunctionVal
	return globals
}

func (d *Driver) MaybeFromJSValue(ctx context.Context, runtime *goja.Runtime, value goja.Value) (any, bool) {
	if IsSQLQuery(runtime, value, d.sqlQueryCtorVal) {
		return SQLQueryFromJSValue(runtime, value), true
	}
	return nil, false
}

func (d *Driver) getTxFromContext(ctx context.Context) *sql.Tx {
	if tx, ok := ctx.Value(txContextKey).(*sql.Tx); ok {
		return tx
	}
	return nil
}

func (d *Driver) renderSQL(sqlTemplate string) (string, error) {
	tmpl, err := template.New("sql").Parse(sqlTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	data := map[string]string{
		"TableName": MIGRATIONS_TABLE,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
