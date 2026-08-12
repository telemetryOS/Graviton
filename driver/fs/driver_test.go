package fs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/telemetryos/graviton/config"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"
)

func setupTestDriver(t *testing.T) (*Driver, context.Context) {
	t.Helper()

	conf := &config.DatabaseConfig{
		Name:          "files",
		Kind:          config.DatabaseKindFS,
		ConnectionUrl: t.TempDir(),
	}

	drv := New(conf)
	ctx := context.Background()

	if err := drv.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	t.Cleanup(func() {
		drv.Disconnect(ctx)
	})

	return drv, ctx
}

func Test_Driver_Connect_CreatesRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "store")
	drv := New(&config.DatabaseConfig{Name: "files", ConnectionUrl: root})

	if err := drv.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("Connect() did not create root directory: %v", err)
	}
}

func Test_Driver_Connect_EmptyRoot(t *testing.T) {
	drv := New(&config.DatabaseConfig{Name: "files", ConnectionUrl: ""})
	if err := drv.Connect(context.Background()); err == nil {
		t.Fatal("Connect() with empty connection_url should error")
	}
}

func Test_Driver_TrackingRoundTrip(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	got, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no applied migrations, got %d", len(got))
	}

	want := []*migrationsmeta.MigrationMetadata{
		{Filename: "20231225010950-one.migration.ts", Source: "source1", AppliedAt: time.Now().UTC().Truncate(time.Second)},
		{Filename: "20231225010956-two.migration.ts", Source: "source2", AppliedAt: time.Now().UTC().Truncate(time.Second)},
	}
	if err := drv.SetAppliedMigrationsMetadata(ctx, want); err != nil {
		t.Fatalf("SetAppliedMigrationsMetadata() error = %v", err)
	}

	got, err = drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d applied migrations, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Filename != want[i].Filename || got[i].Source != want[i].Source || !got[i].AppliedAt.Equal(want[i].AppliedAt) {
			t.Errorf("migration %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	if err := drv.SetAppliedMigrationsMetadata(ctx, nil); err != nil {
		t.Fatalf("SetAppliedMigrationsMetadata(nil) error = %v", err)
	}
	got, err = drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected tracking reset to empty, got %d entries", len(got))
	}
}

func Test_Driver_NoOpTransactions(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	if err := drv.BeginTx(ctx); err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if drv.HasOpenTx() {
		t.Error("HasOpenTx() = true, want false (fs has no transactions)")
	}
	if err := drv.CommitTx(ctx); err != nil {
		t.Fatalf("CommitTx() error = %v", err)
	}
	if err := drv.RollbackTx(ctx); err != nil {
		t.Fatalf("RollbackTx() error = %v", err)
	}
}

func Test_Driver_RenameDatabase(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	if err := os.WriteFile(filepath.Join(drv.root, "keep.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	newRoot := filepath.Join(t.TempDir(), "store__migrated__")
	if err := drv.RenameDatabase(ctx, newRoot); err != nil {
		t.Fatalf("RenameDatabase() error = %v", err)
	}

	if _, err := os.Stat(drv.root); !os.IsNotExist(err) {
		t.Errorf("source root should be gone after rename, stat err = %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(newRoot, "keep.txt")); err != nil || string(data) != "x" {
		t.Errorf("renamed root content = %q, %v; want x", data, err)
	}
}

func Test_Driver_RenameDatabase_Refusals(t *testing.T) {
	drv, ctx := setupTestDriver(t)

	if err := drv.RenameDatabase(ctx, ""); err == nil {
		t.Error("rename to an empty path should error")
	}
	if err := drv.RenameDatabase(ctx, drv.root); err == nil {
		t.Error("rename to itself should error")
	}

	existing := t.TempDir()
	if err := drv.RenameDatabase(ctx, existing); err == nil {
		t.Error("rename onto an existing path should error")
	}
	if _, err := os.Stat(drv.root); err != nil {
		t.Errorf("source root must survive refused renames: %v", err)
	}
}

func Test_Driver_Resolve(t *testing.T) {
	drv, _ := setupTestDriver(t)

	// Paths that stay inside the root resolve under it; absolute paths are
	// treated as root-relative rather than escaping.
	for _, path := range []string{"a.txt", "a/b/c.txt", "/etc/passwd", "", "."} {
		resolved, err := drv.resolve(path)
		if err != nil {
			t.Errorf("resolve(%q) error = %v", path, err)
			continue
		}
		if resolved != drv.root && !strings.HasPrefix(resolved, drv.root+string(filepath.Separator)) {
			t.Errorf("resolve(%q) = %q, escapes root %q", path, resolved, drv.root)
		}
	}

	// .. traversal out of the root must error.
	for _, path := range []string{"..", "../outside", "a/../../outside"} {
		if resolved, err := drv.resolve(path); err == nil {
			t.Errorf("resolve(%q) = %q, want error", path, resolved)
		}
	}
}
