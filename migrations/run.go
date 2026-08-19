package migrations

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

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

	// lock is the whole-run migrations lock this run holds in the tracking
	// database, nil until Lock succeeds.
	lock *migrationsmeta.MigrationsLock
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

// Lock claims the whole-run migrations lock in the tracking database. One lock
// guards the whole project — commands that run migration bodies (up, down,
// set-head) take it after connecting and release it when they finish, so two
// concurrent runs cannot interleave bodies or clobber the tracking list.
func (r *Run) Lock() error {
	lock := migrationsmeta.NewMigrationsLock()
	held, err := r.trackingDriver().AcquireMigrationsLock(r.ctx, lock)
	if err != nil {
		return fmt.Errorf("failed to acquire the migrations lock: %w", err)
	}
	if held != nil {
		return fmt.Errorf(
			"migrations are locked by %s (pid %d) since %s; if that run is no longer alive, clear the lock with `graviton unlock`",
			held.Hostname, held.Pid, held.AcquiredAt.Local().Format(time.RFC3339),
		)
	}
	r.lock = lock
	return nil
}

// Unlock releases the lock taken by Lock. It is a no-op when this run holds no
// lock, and it never releases another run's lock (release is conditional on
// the holder id).
func (r *Run) Unlock() {
	if r.lock == nil {
		return
	}
	if err := r.trackingDriver().ReleaseMigrationsLock(r.ctx, r.lock.Holder); err != nil {
		fmt.Println("WARN: failed to release the migrations lock: " + err.Error())
		return
	}
	r.lock = nil
}

// LockInfo returns the currently held migrations lock, or nil when free.
func (r *Run) LockInfo() (*migrationsmeta.MigrationsLock, error) {
	return r.trackingDriver().GetMigrationsLock(r.ctx)
}

// ClearLock unconditionally removes the migrations lock. It backs
// `graviton unlock`, the recovery path for locks left by crashed runs.
func (r *Run) ClearLock() error {
	return r.trackingDriver().ClearMigrationsLock(r.ctx)
}

// ConnectTracking connects only the migrations_db database. Lock inspection
// and clearing need just the tracking storage, and must keep working when an
// unrelated database is unreachable.
func (r *Run) ConnectTracking() error {
	alias := r.conf.MigrationsDb
	if err := r.drivers[alias].Connect(r.ctx); err != nil {
		return fmt.Errorf("failed to connect to database %q: %w", alias, err)
	}
	return nil
}

// DisconnectTracking disconnects the sole database ConnectTracking opened.
func (r *Run) DisconnectTracking() {
	r.drivers[r.conf.MigrationsDb].Disconnect(r.ctx)
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
// configured database — so collection() (or the SQL or store surface) works
// without use(). When unbound (a multi-database project's root, before use()), a
// direct operation errors and directs the caller to use(alias). Operations
// delegate to the bound driver's kind-appropriate native handle.
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

// databaseRenamer is implemented by driver kinds that support renaming a whole
// database (mongodb, fs, and s3). It renames the driver's own database to a
// literal physical newName — a database name, a filesystem path, or a bucket
// key prefix respectively — as an immediate, non-transactional operation.
type databaseRenamer interface {
	RenameDatabase(ctx context.Context, newName string) error
}

// Rename renames the database this handle is bound to (the one use(alias)
// selected, or the sole database in a single-database project) to the literal
// physical name newName, then drops the source. It is the operation behind the
// retire-databases pattern: a dedicated migration renames a no-longer-used
// database to a __migrated__-suffixed name at cutover, and its down() renames it
// back.
//
// newName is not resolved through config — it is a deliberate literal (a
// mongodb database name, an fs path, or an s3 key prefix), so the renamed
// database leaves config-managed space. Renaming from an unbound
// multi-database root (before use(alias)), renaming the migrations_db, or
// renaming a database with an open transaction on this run errors cleanly.
// rename is supported for mongodb, fs, and s3 databases.
//
// It is immediate and irreversible except by renaming back: it does not run in
// (and is not rolled back with) the migration's transactions, so if the body
// later fails the rename is not undone. Use it in a dedicated retire-databases
// migration, not mixed with transactional writes.
func (h *Handle) Rename(newName string) {
	if h.drv == nil {
		panic(fmt.Errorf(
			"multiple databases are configured (%s); call use(alias) to select one before renaming",
			strings.Join(h.run.order, ", "),
		))
	}
	if h.alias == h.run.conf.MigrationsDb {
		panic(fmt.Errorf("cannot rename the migrations_db database %q", h.alias))
	}
	renamer, ok := h.drv.(databaseRenamer)
	if !ok {
		panic(fmt.Errorf("rename is not supported for database %q; only mongodb, fs, and s3 databases can be renamed", h.alias))
	}
	if err := renamer.RenameDatabase(h.ctx, newName); err != nil {
		panic(err)
	}
}

// streamReader is implemented by driver kinds whose stores can open a
// streaming reader over one item (fs, s3).
type streamReader interface {
	OpenRead(ctx context.Context, path string) (io.ReadCloser, error)
}

// streamWriter is implemented by driver kinds whose stores can write one item
// from a stream in bounded memory (fs, s3).
type streamWriter interface {
	WriteStream(ctx context.Context, path string, r io.Reader) error
}

// CopyTo streams one item from this handle's database into destAlias's
// database without buffering the whole payload in memory — the way to move
// large files between file-like stores (fs ↔ s3), where read()/write() would
// hold the entire content at once. Like every store operation it is immediate
// and non-transactional.
func (h *Handle) CopyTo(destAlias string, src string, dst string) {
	if h.drv == nil {
		panic(fmt.Errorf(
			"multiple databases are configured (%s); call use(alias) to select the source before copyTo()",
			strings.Join(h.run.order, ", "),
		))
	}
	source, ok := h.drv.(streamReader)
	if !ok {
		panic(fmt.Errorf("copyTo() is not available on database %q; the source must be an fs or s3 database", h.alias))
	}

	destDriver, ok := h.run.drivers[destAlias]
	if !ok {
		panic(fmt.Errorf(
			"no database aliased %q is configured; configured databases: %s",
			destAlias, strings.Join(h.run.order, ", "),
		))
	}
	dest, ok := destDriver.(streamWriter)
	if !ok {
		panic(fmt.Errorf("copyTo() cannot write to database %q; the destination must be an fs or s3 database", destAlias))
	}

	reader, err := source.OpenRead(h.ctx, src)
	if err != nil {
		panic(err)
	}
	defer reader.Close()

	if err := dest.WriteStream(h.ctx, dst, reader); err != nil {
		panic(err)
	}
}

func (h *Handle) Collection(name string) any { return h.delegate("Collection", name) }
func (h *Handle) Exec(query any) any         { return h.delegate("Exec", query) }
func (h *Handle) Query(query any) any        { return h.delegate("Query", query) }
func (h *Handle) QueryOne(query any) any     { return h.delegate("QueryOne", query) }

// File-like surfaces (fs, s3). Shared names delegate to whichever of the two
// kinds the handle is bound to; a method the bound kind lacks errors clearly.
func (h *Handle) Read(path string) any            { return h.delegate("Read", path) }
func (h *Handle) ReadBytes(path string) any       { return h.delegate("ReadBytes", path) }
func (h *Handle) Write(path string, data any) any { return h.delegate("Write", path, data) }
func (h *Handle) Remove(path string) any          { return h.delegate("Remove", path) }
func (h *Handle) RemoveAll(path string) any       { return h.delegate("RemoveAll", path) }
func (h *Handle) Mkdir(path string) any           { return h.delegate("Mkdir", path) }
func (h *Handle) List(path string) any            { return h.delegate("List", path) }
func (h *Handle) Exists(path string) any          { return h.delegate("Exists", path) }
func (h *Handle) Copy(src, dst string) any        { return h.delegate("Copy", src, dst) }
func (h *Handle) Move(src, dst string) any        { return h.delegate("Move", src, dst) }
func (h *Handle) Put(key string, data any) any    { return h.delegate("Put", key, data) }
func (h *Handle) GetBytes(key string) any         { return h.delegate("GetBytes", key) }
func (h *Handle) Delete(key string) any           { return h.delegate("Delete", key) }

// Key-value surface (redis). Get is shared with the s3 surface. Ttl (not TTL)
// keeps the JS name ttl — only a method's first letter is lowercased when it is
// surfaced to scripts.
func (h *Handle) Get(key string) any { return h.delegate("Get", key) }
func (h *Handle) Set(key string, value any, ttlSeconds ...int64) any {
	args := []any{key, value}
	for _, ttl := range ttlSeconds {
		args = append(args, ttl)
	}
	return h.delegate("Set", args...)
}
func (h *Handle) Del(keys ...string) any {
	args := make([]any, len(keys))
	for i, key := range keys {
		args[i] = key
	}
	return h.delegate("Del", args...)
}
func (h *Handle) Keys(pattern string) any        { return h.delegate("Keys", pattern) }
func (h *Handle) Expire(key string, s int64) any { return h.delegate("Expire", key, s) }
func (h *Handle) Ttl(key string) any             { return h.delegate("Ttl", key) }
func (h *Handle) HGet(key, field string) any     { return h.delegate("HGet", key, field) }
func (h *Handle) HSet(key, field string, value any) any {
	return h.delegate("HSet", key, field, value)
}
func (h *Handle) HGetAll(key string) any { return h.delegate("HGetAll", key) }
func (h *Handle) HDel(key string, fields ...string) any {
	args := []any{key}
	for _, field := range fields {
		args = append(args, field)
	}
	return h.delegate("HDel", args...)
}
func (h *Handle) SAdd(key string, members ...any) any {
	return h.delegate("SAdd", keyed(key, members)...)
}
func (h *Handle) SRem(key string, members ...any) any {
	return h.delegate("SRem", keyed(key, members)...)
}
func (h *Handle) SMembers(key string) any         { return h.delegate("SMembers", key) }
func (h *Handle) SIsMember(key string, m any) any { return h.delegate("SIsMember", key, m) }
func (h *Handle) LPush(key string, values ...any) any {
	return h.delegate("LPush", keyed(key, values)...)
}
func (h *Handle) RPush(key string, values ...any) any {
	return h.delegate("RPush", keyed(key, values)...)
}
func (h *Handle) LRange(key string, start, stop int64) any {
	return h.delegate("LRange", key, start, stop)
}
func (h *Handle) LLen(key string) any { return h.delegate("LLen", key) }
func (h *Handle) ZAdd(key string, score float64, member string) any {
	return h.delegate("ZAdd", key, score, member)
}
func (h *Handle) ZRem(key string, members ...any) any {
	return h.delegate("ZRem", keyed(key, members)...)
}
func (h *Handle) ZRange(key string, start, stop int64) any {
	return h.delegate("ZRange", key, start, stop)
}
func (h *Handle) ZScore(key string, member string) any { return h.delegate("ZScore", key, member) }
func (h *Handle) Incr(key string) any                  { return h.delegate("Incr", key) }
func (h *Handle) IncrBy(key string, delta int64) any   { return h.delegate("IncrBy", key, delta) }
func (h *Handle) Command(args ...any) any              { return h.delegate("Command", args...) }

// keyed returns the flat argument list for an operation taking a key and a
// variadic tail, the shape delegate forwards.
func keyed(key string, tail []any) []any {
	args := make([]any, 0, len(tail)+1)
	args = append(args, key)
	return append(args, tail...)
}

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
	mt := m.Type()
	in := make([]reflect.Value, len(args))
	for i, a := range args {
		in[i] = argValue(mt, i, a)
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

// DisableTransactions puts every driver that supports it into non-transactional
// mode for the rest of the run, so a migration whose writes cannot fit in one
// transaction can still be applied. Drivers that have no transactions to
// disable are left alone.
//
// This gives up rollback: ApplyMigration's failure path can no longer undo what
// the body already wrote, and a migration that fails partway leaves its partial
// writes behind. The applied marker is still written last, so the migration
// stays unmarked and re-runs — which recovers the run only for migrations that
// are idempotent and convergent. See driver.TransactionDisabler.
func (r *Run) DisableTransactions() {
	for _, alias := range r.order {
		if d, ok := r.drivers[alias].(driver.TransactionDisabler); ok {
			d.DisableTransactions()
		}
	}
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
