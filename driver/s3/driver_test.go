package s3

import (
	"context"
	"testing"
	"time"

	"github.com/telemetryos/graviton/config"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"
)

// setupTestDriver builds a driver over the in-memory fake, bypassing Connect
// (which needs a real endpoint; see integration_test.go for that path).
func setupTestDriver(t *testing.T, connectionUrl string) (*Driver, *fakeS3, context.Context) {
	t.Helper()

	target, err := parseTarget(connectionUrl)
	if err != nil {
		t.Fatalf("parseTarget(%q) error = %v", connectionUrl, err)
	}

	fake := newFakeS3(target.bucket)
	drv := New(&config.DatabaseConfig{
		Name:          "assets",
		Kind:          config.DatabaseKindS3,
		ConnectionUrl: connectionUrl,
	})
	drv.target = target
	drv.client = fake

	return drv, fake, context.Background()
}

func Test_ParseTarget(t *testing.T) {
	target, err := parseTarget("s3://my-bucket/some/prefix?region=us-west-2&endpoint=http://localhost:9000&path-style=true&access-key=ak&secret-key=sk")
	if err != nil {
		t.Fatalf("parseTarget() error = %v", err)
	}
	if target.bucket != "my-bucket" || target.prefix != "some/prefix" {
		t.Errorf("bucket/prefix = %q/%q", target.bucket, target.prefix)
	}
	if target.region != "us-west-2" || target.endpoint != "http://localhost:9000" || !target.pathStyle {
		t.Errorf("region/endpoint/pathStyle = %q/%q/%v", target.region, target.endpoint, target.pathStyle)
	}
	if target.accessKey != "ak" || target.secretKey != "sk" {
		t.Errorf("credentials not parsed")
	}

	if _, err := parseTarget("s3://bucket-only"); err != nil {
		t.Errorf("bucket-only URL should parse: %v", err)
	}
	if _, err := parseTarget("http://not-s3/bucket"); err == nil {
		t.Error("non-s3 scheme should error")
	}
	if _, err := parseTarget("s3://"); err == nil {
		t.Error("missing bucket should error")
	}
}

func Test_FullAndRelativeKeys(t *testing.T) {
	drv, _, _ := setupTestDriver(t, "s3://bucket/data")

	if got := drv.fullKey("a/b.txt"); got != "data/a/b.txt" {
		t.Errorf("fullKey() = %q, want data/a/b.txt", got)
	}
	if got := drv.relativeKey("data/a/b.txt"); got != "a/b.txt" {
		t.Errorf("relativeKey() = %q, want a/b.txt", got)
	}

	unprefixed, _, _ := setupTestDriver(t, "s3://bucket")
	if got := unprefixed.fullKey("a.txt"); got != "a.txt" {
		t.Errorf("fullKey() without prefix = %q, want a.txt", got)
	}
}

func Test_Driver_TrackingRoundTrip(t *testing.T) {
	drv, fake, ctx := setupTestDriver(t, "s3://bucket/data")

	got, err := drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no applied migrations, got %d", len(got))
	}

	want := []*migrationsmeta.MigrationMetadata{
		{Filename: "20231225010950-one.migration.ts", Source: "source1", AppliedAt: time.Now().UTC().Truncate(time.Second)},
	}
	if err := drv.SetAppliedMigrationsMetadata(ctx, want); err != nil {
		t.Fatalf("SetAppliedMigrationsMetadata() error = %v", err)
	}

	if _, ok := fake.objects["data/graviton-migrations.json"]; !ok {
		t.Error("tracking object not stored under the configured prefix")
	}

	got, err = drv.GetAppliedMigrationsMetadata(ctx)
	if err != nil {
		t.Fatalf("GetAppliedMigrationsMetadata() error = %v", err)
	}
	if len(got) != 1 || got[0].Filename != want[0].Filename || got[0].Source != want[0].Source || !got[0].AppliedAt.Equal(want[0].AppliedAt) {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func Test_Driver_RenameDatabase(t *testing.T) {
	drv, fake, ctx := setupTestDriver(t, "s3://bucket/data")

	fake.objects["data/a.txt"] = []byte("a")
	fake.objects["data/sub/b.txt"] = []byte("b")
	fake.objects["unrelated/c.txt"] = []byte("c")

	if err := drv.RenameDatabase(ctx, "data__migrated__"); err != nil {
		t.Fatalf("RenameDatabase() error = %v", err)
	}

	if string(fake.objects["data__migrated__/a.txt"]) != "a" || string(fake.objects["data__migrated__/sub/b.txt"]) != "b" {
		t.Errorf("objects not moved to the new prefix: %v", fake.objects)
	}
	if _, exists := fake.objects["data/a.txt"]; exists {
		t.Error("source objects survive rename")
	}
	if _, exists := fake.objects["unrelated/c.txt"]; !exists {
		t.Error("objects outside the prefix must be untouched")
	}
}

func Test_Driver_RenameDatabase_Refusals(t *testing.T) {
	drv, fake, ctx := setupTestDriver(t, "s3://bucket/data")
	fake.objects["data/a.txt"] = []byte("a")

	if err := drv.RenameDatabase(ctx, ""); err == nil {
		t.Error("rename to an empty prefix should error")
	}
	if err := drv.RenameDatabase(ctx, "data"); err == nil {
		t.Error("rename to itself should error")
	}

	fake.objects["taken/x.txt"] = []byte("x")
	if err := drv.RenameDatabase(ctx, "taken"); err == nil {
		t.Error("rename onto a non-empty prefix should error")
	}
	if _, exists := fake.objects["data/a.txt"]; !exists {
		t.Error("source objects must survive refused renames")
	}

	unprefixed, _, _ := setupTestDriver(t, "s3://bucket")
	if err := unprefixed.RenameDatabase(ctx, "anything"); err == nil {
		t.Error("rename of a prefixless database should error")
	}
}

func Test_Driver_NoOpTransactions(t *testing.T) {
	drv, _, ctx := setupTestDriver(t, "s3://bucket")

	if err := drv.BeginTx(ctx); err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if drv.HasOpenTx() {
		t.Error("HasOpenTx() = true, want false (s3 has no transactions)")
	}
	if err := drv.CommitTx(ctx); err != nil {
		t.Fatalf("CommitTx() error = %v", err)
	}
	if err := drv.RollbackTx(ctx); err != nil {
		t.Fatalf("RollbackTx() error = %v", err)
	}
}
