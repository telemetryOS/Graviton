package migrations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/telemetryos/graviton/config"
	"github.com/telemetryos/graviton/driver"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"
)

// Run is one Graviton invocation over the whole system: every configured
// database is connected once, migrations form a single linear ordered set, and
// applied-migration tracking lives in the single migrations_db entry. A Run owns
// one driver instance per configured database (one session per database per run
// for kinds that have sessions), so a migration can interleave writes across
// databases and the runner can commit each as an independent per-handle
// transaction, writing the applied marker last.
type Run struct {
	ctx     context.Context
	conf    *config.Config
	drivers map[string]driver.Driver
	order   []string
}

func NewRun(ctx context.Context, conf *config.Config) *Run {
	r := &Run{
		ctx:     ctx,
		conf:    conf,
		drivers: make(map[string]driver.Driver, len(conf.Databases)),
	}
	for _, dc := range conf.Databases {
		r.drivers[dc.Name] = driver.FromDatabaseConfig(dc)
		r.order = append(r.order, dc.Name)
	}
	return r
}

// Connect opens every configured database. Every database may be touched by a
// migration body, so all are connected up front.
func (r *Run) Connect() error {
	for _, alias := range r.order {
		if err := r.drivers[alias].Connect(r.ctx); err != nil {
			return fmt.Errorf("failed to connect to database %q: %w", alias, err)
		}
	}
	return nil
}

func (r *Run) Disconnect() {
	for _, alias := range r.order {
		r.drivers[alias].Disconnect(r.ctx)
	}
}

// trackingDriver is the driver for the migrations_db entry that holds the linear
// applied-migrations metadata.
func (r *Run) trackingDriver() driver.Driver {
	return r.drivers[r.conf.MigrationsDb]
}

func (r *Run) driversList() []driver.Driver {
	list := make([]driver.Driver, 0, len(r.order))
	for _, alias := range r.order {
		list = append(list, r.drivers[alias])
	}
	return list
}

// Handle is the migration-facing root handle (__g__). It carries an optional
// bound database: use(alias) returns a new Handle bound to that database, and in
// a single-database project the root Handle is already bound to the only
// configured database — so collection() (or the SQL surface) works without
// use(). When unbound (a multi-database project's root, before use()), a direct
// operation errors and directs the caller to use(alias). Operations delegate to
// the bound driver's kind-appropriate native handle.
type Handle struct {
	ctx   context.Context
	run   *Run
	alias string
	drv   driver.Driver
}

func (h *Handle) Use(alias string) *Handle {
	d, ok := h.run.drivers[alias]
	if !ok {
		panic(fmt.Errorf(
			"no database aliased %q is configured; configured databases: %s",
			alias, strings.Join(h.run.order, ", "),
		))
	}
	return &Handle{ctx: h.ctx, run: h.run, alias: alias, drv: d}
}

func (h *Handle) Collection(name string) any { return h.delegate("Collection", name) }
func (h *Handle) Exec(query any) any         { return h.delegate("Exec", query) }
func (h *Handle) Query(query any) any        { return h.delegate("Query", query) }
func (h *Handle) QueryOne(query any) any     { return h.delegate("QueryOne", query) }

// delegate forwards an operation to the bound database's native handle. An
// unbound handle (multi-database root) errors, telling the caller to use(alias).
// A method the bound kind does not provide (e.g. collection() on a SQL database)
// errors clearly rather than silently missing.
func (h *Handle) delegate(method string, args ...any) any {
	if h.drv == nil {
		panic(fmt.Errorf(
			"multiple databases are configured (%s); call use(alias) to select one before running operations",
			strings.Join(h.run.order, ", "),
		))
	}
	native := reflect.ValueOf(h.drv.Handle(h.ctx))
	m := native.MethodByName(method)
	if !m.IsValid() {
		jsName := strings.ToLower(method[:1]) + method[1:]
		panic(fmt.Errorf("%s() is not available on database %q", jsName, h.alias))
	}
	in := make([]reflect.Value, len(args))
	for i, a := range args {
		in[i] = reflect.ValueOf(a)
	}
	out := m.Call(in)
	if len(out) == 0 {
		return nil
	}
	return out[0].Interface()
}

// rootHandle builds the JS root handle. With exactly one configured database it
// is bound to that database directly; with more than one it is unbound and
// use(alias) is required before any operation.
func (r *Run) rootHandle() *Handle {
	h := &Handle{ctx: r.ctx, run: r}
	if len(r.order) == 1 {
		h.alias = r.order[0]
		h.drv = r.drivers[r.order[0]]
	}
	return h
}

func (r *Run) newScript(src, origin string) *Script {
	script := &Script{
		ctx:     r.ctx,
		drivers: r.driversList(),
		handle:  r.rootHandle(),
		src:     src,
		origin:  origin,
	}
	script.Evaluate()
	return script
}

func (r *Run) compileScript(origin, path string) (*Script, error) {
	src, buildErr := buildMigrationSource(path)
	if buildErr != nil {
		return nil, buildErr
	}
	return r.newScript(src, origin), nil
}

func (r *Run) migrationsDir() string {
	return filepath.Join(r.conf.ProjectPath, r.conf.MigrationsPath)
}

// GetPending returns the migrations present on disk that are not yet recorded in
// the tracking database, in linear filename order.
func (r *Run) GetPending() ([]*Migration, error) {
	appliedMigrationsMetadata, err := r.trackingDriver().GetAppliedMigrationsMetadata(r.ctx)
	if err != nil {
		return nil, err
	}
	appliedMigrationsFilenames := make(map[string]bool)
	for _, appliedMigrationMetadata := range appliedMigrationsMetadata {
		appliedMigrationsFilenames[appliedMigrationMetadata.Filename] = true
	}

	migrationsPath := r.migrationsDir()
	migrationsDir, err := os.ReadDir(migrationsPath)
	if err != nil {
		return nil, err
	}

	var pendingMigrations []*Migration
	for _, migrationDir := range migrationsDir {
		migrationFilename := migrationDir.Name()
		if appliedMigrationsFilenames[migrationFilename] || !migrationDir.Type().IsRegular() || !migrationsmeta.MigrationNamePattern.MatchString(migrationFilename) {
			continue
		}

		migrationPath := filepath.Join(migrationsPath, migrationFilename)
		script, err := r.compileScript(migrationFilename, migrationPath)
		if err != nil {
			return nil, err
		}

		pendingMigrations = append(pendingMigrations, &Migration{
			MigrationMetadata: &migrationsmeta.MigrationMetadata{
				Filename: migrationFilename,
				Source:   script.src,
			},
			Script: script,
		})
	}

	return pendingMigrations, nil
}

// AppliedMetadata returns the raw applied-migrations tracking list, in linear
// filename order, without reconstructing any scripts. Commands use it to build
// the marker list they persist as migrations are applied or rolled back.
func (r *Run) AppliedMetadata() ([]*migrationsmeta.MigrationMetadata, error) {
	return r.trackingDriver().GetAppliedMigrationsMetadata(r.ctx)
}

// GetApplied returns the applied migrations, reconstructing their scripts from
// the source stored in the tracking database.
func (r *Run) GetApplied() ([]*Migration, error) {
	appliedMigrationsMetadata, err := r.trackingDriver().GetAppliedMigrationsMetadata(r.ctx)
	if err != nil {
		return nil, err
	}

	var appliedMigrations []*Migration
	for _, appliedMigrationMetadata := range appliedMigrationsMetadata {
		appliedMigrations = append(appliedMigrations, &Migration{
			MigrationMetadata: appliedMigrationMetadata,
			Script:            r.newScript(appliedMigrationMetadata.Source, appliedMigrationMetadata.Filename),
		})
	}

	return appliedMigrations, nil
}

// GetAppliedWithDownFuncFromDisk returns the applied migrations with their down
// functions compiled from the current files on disk rather than the stored
// source.
func (r *Run) GetAppliedWithDownFuncFromDisk() ([]*Migration, error) {
	appliedMigrationsMetadata, err := r.trackingDriver().GetAppliedMigrationsMetadata(r.ctx)
	if err != nil {
		return nil, err
	}

	migrationsPath := r.migrationsDir()

	var appliedMigrations []*Migration
	for _, appliedMigrationMetadata := range appliedMigrationsMetadata {
		migrationPath := filepath.Join(migrationsPath, appliedMigrationMetadata.Filename)

		stat, err := os.Stat(migrationPath)
		if err != nil {
			return nil, err
		}
		if !stat.Mode().IsRegular() {
			fmt.Println(
				"Could not collect the necessary down functions for applied migrations " +
					"on disk. Missing migration file `" + appliedMigrationMetadata.Filename +
					"` from migrations directory `" + r.conf.MigrationsPath + "`",
			)
			os.Exit(1)
		}

		script, err := r.compileScript(appliedMigrationMetadata.Filename, migrationPath)
		if err != nil {
			return nil, err
		}

		appliedMigrations = append(appliedMigrations, &Migration{
			MigrationMetadata: appliedMigrationMetadata,
			Script:            script,
		})
	}

	return appliedMigrations, nil
}

// ApplyMigration runs a migration body and, on success, commits every open data
// transaction and then writes the applied marker last in its own transaction.
//
// The recovery model is idempotent/convergent migrations plus re-run: because
// the marker is written strictly after the data commits (and in a separate
// transaction), a process that dies after some data commits but before the
// marker leaves the migration unmarked, so it re-runs. Cross-database atomicity
// is per-handle, not joint — this is deliberate.
//
// markerList is the full applied-migrations list to persist once the body and
// its data commits succeed.
func (r *Run) ApplyMigration(body func() error, markerList []*migrationsmeta.MigrationMetadata) error {
	if err := runBody(body); err != nil {
		r.rollbackAll()
		return err
	}

	if err := r.commitDataTransactions(); err != nil {
		// Databases committed before the failure stay committed; roll back the
		// rest and leave the marker unwritten so the migration re-runs.
		r.rollbackAll()
		return err
	}

	return r.writeMarker(markerList)
}

// SetHead writes the applied-migrations tracking list without running any
// bodies. It is used by set-head to move the recorded head directly.
func (r *Run) SetHead(markerList []*migrationsmeta.MigrationMetadata) error {
	return r.writeMarker(markerList)
}

// runBody runs a migration body, converting a panic from the JS-facing
// operations (which panic on driver errors) into a returned error.
func runBody(body func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("panic in migration: %v", r)
			}
		}
	}()
	return body()
}

func (r *Run) commitDataTransactions() error {
	for _, alias := range r.order {
		d := r.drivers[alias]
		if !d.HasOpenTx() {
			continue
		}
		if err := d.CommitTx(r.ctx); err != nil {
			return fmt.Errorf("failed to commit transaction for database %q: %w", alias, err)
		}
	}
	return nil
}

func (r *Run) rollbackAll() {
	for _, alias := range r.order {
		r.drivers[alias].RollbackTx(r.ctx)
	}
}

// writeMarker persists the applied-migrations list to the tracking database in
// its own transaction, strictly after all data transactions have committed.
func (r *Run) writeMarker(markerList []*migrationsmeta.MigrationMetadata) error {
	d := r.trackingDriver()
	if err := d.BeginTx(r.ctx); err != nil {
		return err
	}
	if err := d.SetAppliedMigrationsMetadata(r.ctx, markerList); err != nil {
		d.RollbackTx(r.ctx)
		return err
	}
	return d.CommitTx(r.ctx)
}
