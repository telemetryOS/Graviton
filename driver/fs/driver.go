// Package fs is the local-filesystem driver. A configured "database" is a root
// directory; migration scripts read, write, move, and delete files under it.
// It exists so file stores can take part in the same linear migration set as
// databases — e.g. relocating uploaded assets while the rows that reference
// them are rewritten.
//
// Filesystems have no transactions. Every handle operation applies
// immediately: BeginTx/CommitTx/RollbackTx are no-ops, so a failed migration
// body does NOT undo file writes that already happened. This matches
// Graviton's recovery model — idempotent/convergent migrations plus re-run —
// and mirrors how the mongodb rename() operation is documented.
package fs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/telemetryos/graviton/config"
	"github.com/telemetryos/graviton/driver/internal/jsbytes"
	"github.com/telemetryos/graviton/driver/internal/jsontracking"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"github.com/dop251/goja"
)

// MIGRATIONS_FILE is the tracking document, stored at the root of the
// configured directory when this driver is the migrations_db.
const MIGRATIONS_FILE = "graviton-migrations.json"

type Driver struct {
	config *config.DatabaseConfig

	// root is the absolute configured directory; every handle path resolves
	// inside it.
	root string
}

// New builds a filesystem driver for conf. connection_url is the root
// directory (an absolute or working-directory-relative path, optionally with a
// file:// prefix); database_name is unused.
func New(conf *config.DatabaseConfig) *Driver {
	return &Driver{config: conf}
}

func (d *Driver) Connect(ctx context.Context) error {
	root := strings.TrimPrefix(d.config.ConnectionUrl, "file://")
	if root == "" {
		return fmt.Errorf("fs database %q has no root directory in connection_url", d.config.Name)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("failed to resolve fs root %q: %w", root, err)
	}

	if err := os.MkdirAll(absRoot, 0755); err != nil {
		return fmt.Errorf("failed to create fs root %q: %w", absRoot, err)
	}

	d.root = absRoot
	return nil
}

func (d *Driver) Disconnect(ctx context.Context) error {
	return nil
}

func (d *Driver) GetAppliedMigrationsMetadata(ctx context.Context) ([]*migrationsmeta.MigrationMetadata, error) {
	data, err := os.ReadFile(filepath.Join(d.root, MIGRATIONS_FILE))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return jsontracking.Unmarshal(data)
}

// SetAppliedMigrationsMetadata writes the tracking document atomically
// (temp file + rename) so a crash mid-write cannot leave a torn list.
func (d *Driver) SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []*migrationsmeta.MigrationMetadata) error {
	data, err := jsontracking.Marshal(migrationsMetadata)
	if err != nil {
		return err
	}

	target := filepath.Join(d.root, MIGRATIONS_FILE)
	tmp, err := os.CreateTemp(d.root, MIGRATIONS_FILE+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, target)
}

// BeginTx is a no-op: filesystems have no transactions, so handle operations
// apply immediately and are not rolled back on failure.
func (d *Driver) BeginTx(ctx context.Context) error { return nil }

// CommitTx is a no-op; see BeginTx.
func (d *Driver) CommitTx(ctx context.Context) error { return nil }

// RollbackTx is a no-op; see BeginTx.
func (d *Driver) RollbackTx(ctx context.Context) error { return nil }

// HasOpenTx always reports false; see BeginTx.
func (d *Driver) HasOpenTx() bool { return false }

func (d *Driver) Handle(ctx context.Context) any {
	return &Handle{ctx: ctx, driver: d}
}

func (d *Driver) Init(ctx context.Context, runtime *goja.Runtime) {}

func (d *Driver) Globals(ctx context.Context, runtime *goja.Runtime) map[string]any {
	return map[string]any{}
}

func (d *Driver) MaybeFromJSValue(ctx context.Context, jsvm *goja.Runtime, value goja.Value) (any, bool) {
	return nil, false
}

// MaybeIntoJSValue surfaces readBytes() results as ArrayBuffer values.
func (d *Driver) MaybeIntoJSValue(ctx context.Context, jsvm *goja.Runtime, value any) (goja.Value, bool) {
	return jsbytes.MaybeIntoJSValue(jsvm, value)
}

// RenameDatabase moves this driver's root directory to newName (a literal
// filesystem path, absolute or working-directory-relative) — the
// retire-databases pattern for file stores. It refuses to overwrite an
// existing target, and os.Rename keeps the move atomic on one filesystem
// (moving across filesystems errors rather than falling back to a copy).
//
// Like the mongodb rename, it is immediate and non-transactional: it is not
// rolled back if the migration body later fails, and the driver still points
// at the old (now missing) root afterwards — keep it in a dedicated
// retire-databases migration.
func (d *Driver) RenameDatabase(ctx context.Context, newName string) error {
	newRoot := strings.TrimPrefix(newName, "file://")
	if newRoot == "" {
		return errors.New("cannot rename database to an empty path")
	}
	newRoot, err := filepath.Abs(newRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve rename target %q: %w", newName, err)
	}
	if newRoot == d.root {
		return fmt.Errorf("cannot rename database %q to itself", d.root)
	}
	if _, err := os.Stat(newRoot); err == nil {
		return fmt.Errorf("rename target %q already exists, refusing to overwrite it", newRoot)
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(newRoot), 0755); err != nil {
		return err
	}
	return os.Rename(d.root, newRoot)
}

// resolve maps a script-supplied path (slash-separated, relative to the
// configured root) to an absolute path, rejecting anything that escapes the
// root via .. (absolute paths are treated as root-relative). This guards
// against accidental escapes in migration scripts, which are trusted code —
// it is a correctness fence, not a security boundary, so symlinks inside the
// root are not chased.
func (d *Driver) resolve(path string) (string, error) {
	resolved := filepath.Clean(filepath.Join(d.root, filepath.FromSlash(path)))
	if resolved != d.root && !strings.HasPrefix(resolved, d.root+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the configured root of fs database %q", path, d.config.Name)
	}
	return resolved, nil
}
