package s3

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/telemetryos/graviton/config"
	"github.com/telemetryos/graviton/driver/internal/locktest"
)

// Test_Integration_RealEndpoint exercises Connect and the handle against a
// real S3-compatible endpoint. Point GRAVITON_TEST_S3_URL at a disposable
// bucket, e.g. a local MinIO:
//
//	GRAVITON_TEST_S3_URL='s3://graviton-test?endpoint=http://localhost:9000&path-style=true&access-key=minioadmin&secret-key=minioadmin'
func Test_Integration_RealEndpoint(t *testing.T) {
	url := os.Getenv("GRAVITON_TEST_S3_URL")
	if url == "" {
		t.Skip("GRAVITON_TEST_S3_URL not set")
	}

	drv := New(&config.DatabaseConfig{
		Name:          "assets",
		Kind:          config.DatabaseKindS3,
		ConnectionUrl: url,
	})
	ctx := context.Background()

	if err := drv.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer drv.Disconnect(ctx)

	// The migrations lock rides on conditional puts (If-None-Match), so run
	// the full lock contract against the real endpoint.
	locktest.Run(t, ctx, drv)

	h := drv.Handle(ctx).(*Handle)

	h.Put("graviton-integration/probe.txt", "payload")
	defer h.Delete("graviton-integration/probe.txt")

	if !h.Exists("graviton-integration/probe.txt") {
		t.Error("Exists() = false after Put")
	}
	if got := h.Get("graviton-integration/probe.txt"); got != "payload" {
		t.Errorf("Get() = %q, want payload", got)
	}

	keys := h.List("graviton-integration")
	if len(keys) != 1 || keys[0] != "graviton-integration/probe.txt" {
		t.Errorf("List() = %v", keys)
	}

	// Server-side copy and move, including a key with reserved characters to
	// exercise CopySource escaping.
	h.Copy("graviton-integration/probe.txt", "graviton-integration/copies/probe copy+1.txt")
	defer h.Delete("graviton-integration/copies/probe copy+1.txt")
	if got := h.Get("graviton-integration/copies/probe copy+1.txt"); got != "payload" {
		t.Errorf("copied content = %q, want payload", got)
	}

	h.Move("graviton-integration/copies/probe copy+1.txt", "graviton-integration/moved.txt")
	defer h.Delete("graviton-integration/moved.txt")
	if h.Exists("graviton-integration/copies/probe copy+1.txt") {
		t.Error("source should be gone after Move")
	}
	if got := h.GetBytes("graviton-integration/moved.txt"); string(got) != "payload" {
		t.Errorf("moved content = %q, want payload", got)
	}

	// Streamed write large enough to take the real multipart path.
	payload := bytes.Repeat([]byte("0123456789abcdef"), 512*1024) // 8 MiB
	if err := drv.WriteStream(ctx, "graviton-integration/big.bin", bytes.NewReader(payload)); err != nil {
		t.Fatalf("WriteStream() error = %v", err)
	}
	defer h.Delete("graviton-integration/big.bin")

	reader, err := drv.OpenRead(ctx, "graviton-integration/big.bin")
	if err != nil {
		t.Fatalf("OpenRead() error = %v", err)
	}
	defer reader.Close()
	roundTripped, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(roundTripped, payload) {
		t.Errorf("multipart round trip = %d bytes, %v; want %d bytes intact", len(roundTripped), err, len(payload))
	}
}
