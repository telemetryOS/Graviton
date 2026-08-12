package s3

import (
	"bytes"
	"io"
	"testing"
)

func Test_Driver_StreamRoundTrip(t *testing.T) {
	drv, fake, ctx := setupTestDriver(t, "s3://bucket/data")

	if err := drv.WriteStream(ctx, "small.txt", bytes.NewReader([]byte("payload"))); err != nil {
		t.Fatalf("WriteStream() error = %v", err)
	}
	if string(fake.objects["data/small.txt"]) != "payload" {
		t.Errorf("streamed object = %q, want payload", fake.objects["data/small.txt"])
	}

	reader, err := drv.OpenRead(ctx, "small.txt")
	if err != nil {
		t.Fatalf("OpenRead() error = %v", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "payload" {
		t.Errorf("streamed read = %q, %v; want payload", data, err)
	}
}

func Test_Driver_StreamMultipart(t *testing.T) {
	drv, fake, ctx := setupTestDriver(t, "s3://bucket/data")

	// Larger than the upload manager's 5 MiB part size, so the write goes
	// through the multipart path (create/upload-part/complete).
	payload := bytes.Repeat([]byte("0123456789abcdef"), 768*1024) // 12 MiB
	if err := drv.WriteStream(ctx, "big.bin", bytes.NewReader(payload)); err != nil {
		t.Fatalf("WriteStream() error = %v", err)
	}

	stored := fake.objects["data/big.bin"]
	if !bytes.Equal(stored, payload) {
		t.Errorf("multipart object = %d bytes, want %d bytes intact", len(stored), len(payload))
	}
	if len(fake.uploads) != 0 {
		t.Errorf("%d multipart uploads left open after completion", len(fake.uploads))
	}
}
