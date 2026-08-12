package redis

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/telemetryos/graviton/config"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"
)

// testDatabaseURL points the tests at a local Redis/Valkey; logical database
// 15 keeps them away from real data. GRAVITON_TEST_REDIS_URL overrides it.
const testDatabaseURL = "redis://localhost:6379/15"

func setupTestDriver(t *testing.T) (*Driver, context.Context) {
	t.Helper()

	url := testDatabaseURL
	if fromEnv := os.Getenv("GRAVITON_TEST_REDIS_URL"); fromEnv != "" {
		url = fromEnv
	}

	conf := &config.DatabaseConfig{
		Name:          "cache",
		Kind:          config.DatabaseKindRedis,
		ConnectionUrl: url,
	}

	drv := New(conf)
	ctx := context.Background()

	if err := drv.Connect(ctx); err != nil {
		t.Skipf("Redis not available on localhost: %v", err)
	}

	if err := drv.client.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("Failed to flush test database: %v", err)
	}

	t.Cleanup(func() {
		drv.client.FlushDB(context.Background())
		drv.Disconnect(ctx)
	})

	return drv, ctx
}

func Test_Driver_Connect_BadURL(t *testing.T) {
	drv := New(&config.DatabaseConfig{Name: "cache", ConnectionUrl: "not-a-redis-url"})
	if err := drv.Connect(context.Background()); err == nil {
		t.Fatal("Connect() with an invalid URL should error")
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
		t.Error("HasOpenTx() = true, want false (redis driver has no transactions)")
	}
	if err := drv.CommitTx(ctx); err != nil {
		t.Fatalf("CommitTx() error = %v", err)
	}
	if err := drv.RollbackTx(ctx); err != nil {
		t.Fatalf("RollbackTx() error = %v", err)
	}
}

func Test_Handle_GetSetDel(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	h := drv.Handle(ctx).(*Handle)

	if got := h.Get("missing"); got != nil {
		t.Errorf("Get(missing) = %v, want nil", got)
	}

	h.Set("greeting", "hello")
	if got := h.Get("greeting"); got != "hello" {
		t.Errorf("Get(greeting) = %v, want hello", got)
	}

	if !h.Exists("greeting") {
		t.Error("Exists(greeting) = false, want true")
	}

	if deleted := h.Del("greeting", "missing"); deleted != 1 {
		t.Errorf("Del() = %d, want 1", deleted)
	}
	if h.Exists("greeting") {
		t.Error("Exists(greeting) = true after Del")
	}
}

func Test_Handle_TTL(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	h := drv.Handle(ctx).(*Handle)

	h.Set("ephemeral", "x", 120)
	if ttl := h.Ttl("ephemeral"); ttl <= 0 || ttl > 120 {
		t.Errorf("Ttl(ephemeral) = %d, want (0, 120]", ttl)
	}

	h.Set("persistent", "x")
	if ttl := h.Ttl("persistent"); ttl != -1 {
		t.Errorf("Ttl(persistent) = %d, want -1", ttl)
	}
	if ttl := h.Ttl("missing"); ttl != -2 {
		t.Errorf("Ttl(missing) = %d, want -2", ttl)
	}

	if !h.Expire("persistent", 60) {
		t.Error("Expire(persistent) = false, want true")
	}
	if ttl := h.Ttl("persistent"); ttl <= 0 || ttl > 60 {
		t.Errorf("Ttl(persistent) = %d after Expire, want (0, 60]", ttl)
	}
	if h.Expire("missing", 60) {
		t.Error("Expire(missing) = true, want false")
	}
}

func Test_Handle_Keys(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	h := drv.Handle(ctx).(*Handle)

	h.Set("app:1", "a")
	h.Set("app:2", "b")
	h.Set("other", "c")

	keys := h.Keys("app:*")
	if len(keys) != 2 {
		t.Errorf("Keys(app:*) = %v, want 2 keys", keys)
	}
}

func Test_Handle_Hashes(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	h := drv.Handle(ctx).(*Handle)

	if got := h.HGet("user:1", "name"); got != nil {
		t.Errorf("HGet(missing) = %v, want nil", got)
	}

	h.HSet("user:1", "name", "Alice")
	h.HSet("user:1", "email", "alice@example.com")

	if got := h.HGet("user:1", "name"); got != "Alice" {
		t.Errorf("HGet(name) = %v, want Alice", got)
	}

	all := h.HGetAll("user:1")
	if len(all) != 2 || all["email"] != "alice@example.com" {
		t.Errorf("HGetAll() = %v", all)
	}

	if deleted := h.HDel("user:1", "email", "missing"); deleted != 1 {
		t.Errorf("HDel() = %d, want 1", deleted)
	}
}

func Test_Handle_Sets(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	h := drv.Handle(ctx).(*Handle)

	if added := h.SAdd("tags", "a", "b", "b"); added != 2 {
		t.Errorf("SAdd() = %d, want 2", added)
	}
	if !h.SIsMember("tags", "a") || h.SIsMember("tags", "missing") {
		t.Error("SIsMember() membership wrong")
	}
	members := h.SMembers("tags")
	if len(members) != 2 {
		t.Errorf("SMembers() = %v, want 2 members", members)
	}
	if removed := h.SRem("tags", "a", "missing"); removed != 1 {
		t.Errorf("SRem() = %d, want 1", removed)
	}
	if len(h.SMembers("missing")) != 0 {
		t.Error("SMembers(missing) should be empty")
	}
}

func Test_Handle_Lists(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	h := drv.Handle(ctx).(*Handle)

	h.RPush("queue", "b", "c")
	if length := h.LPush("queue", "a"); length != 3 {
		t.Errorf("LPush() = %d, want 3", length)
	}
	if length := h.LLen("queue"); length != 3 {
		t.Errorf("LLen() = %d, want 3", length)
	}
	values := h.LRange("queue", 0, -1)
	if len(values) != 3 || values[0] != "a" || values[2] != "c" {
		t.Errorf("LRange() = %v, want [a b c]", values)
	}
}

func Test_Handle_SortedSets(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	h := drv.Handle(ctx).(*Handle)

	if !h.ZAdd("board", 2, "second") {
		t.Error("ZAdd(new member) = false, want true")
	}
	h.ZAdd("board", 1, "first")
	if h.ZAdd("board", 3, "first") {
		t.Error("ZAdd(existing member) = true, want false (score update)")
	}

	members := h.ZRange("board", 0, -1)
	if len(members) != 2 || members[0] != "second" || members[1] != "first" {
		t.Errorf("ZRange() = %v, want [second first] after score update", members)
	}
	if score := h.ZScore("board", "first"); score != 3.0 {
		t.Errorf("ZScore(first) = %v, want 3", score)
	}
	if score := h.ZScore("board", "missing"); score != nil {
		t.Errorf("ZScore(missing) = %v, want nil", score)
	}
	if removed := h.ZRem("board", "first", "missing"); removed != 1 {
		t.Errorf("ZRem() = %d, want 1", removed)
	}
}

func Test_Handle_Counters(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	h := drv.Handle(ctx).(*Handle)

	if value := h.Incr("hits"); value != 1 {
		t.Errorf("Incr() = %d, want 1", value)
	}
	if value := h.IncrBy("hits", 10); value != 11 {
		t.Errorf("IncrBy() = %d, want 11", value)
	}
}

func Test_Handle_Keys_ManyKeys(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	h := drv.Handle(ctx).(*Handle)

	// Enough keys to force several SCAN pages.
	for i := 0; i < 500; i++ {
		h.Set(fmt.Sprintf("bulk:%03d", i), "x")
	}
	h.Set("other", "x")

	keys := h.Keys("bulk:*")
	if len(keys) != 500 {
		t.Errorf("Keys(bulk:*) returned %d keys, want 500", len(keys))
	}
}

func Test_Handle_Command(t *testing.T) {
	drv, ctx := setupTestDriver(t)
	h := drv.Handle(ctx).(*Handle)

	h.Set("old-name", "v")
	h.Command("RENAME", "old-name", "new-name")
	if got := h.Get("new-name"); got != "v" {
		t.Errorf("Get(new-name) = %v, want v after RENAME", got)
	}

	if got := h.Command("GET", "definitely-missing"); got != nil {
		t.Errorf("Command(GET missing) = %v, want nil", got)
	}
}
