package s3

import (
	"testing"
)

func setupTestHandle(t *testing.T) (*Handle, *fakeS3) {
	t.Helper()
	drv, fake, ctx := setupTestDriver(t, "s3://bucket/data")
	return drv.Handle(ctx).(*Handle), fake
}

func expectPanic(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s should panic", what)
		}
	}()
	fn()
}

func Test_Handle_PutGet(t *testing.T) {
	h, fake := setupTestHandle(t)

	h.Put("docs/readme.txt", "hello")
	if _, ok := fake.objects["data/docs/readme.txt"]; !ok {
		t.Error("object not stored under the configured prefix")
	}
	if got := h.Get("docs/readme.txt"); got != "hello" {
		t.Errorf("Get() = %q, want hello", got)
	}

	h.Put("bin/blob", []byte{0x00, 0xff})
	got := h.GetBytes("bin/blob")
	if len(got) != 2 || got[0] != 0x00 || got[1] != 0xff {
		t.Errorf("GetBytes() = %v, want [0 255]", got)
	}

	expectPanic(t, "get() of a missing key", func() {
		h.Get("missing")
	})
	expectPanic(t, "put() with a number body", func() {
		h.Put("x", 42)
	})
}

func Test_Handle_ExistsDelete(t *testing.T) {
	h, _ := setupTestHandle(t)

	if h.Exists("missing") {
		t.Error("Exists(missing) = true, want false")
	}

	h.Put("a.txt", "x")
	if !h.Exists("a.txt") {
		t.Error("Exists(a.txt) = false, want true")
	}

	h.Delete("a.txt")
	if h.Exists("a.txt") {
		t.Error("Exists(a.txt) = true after Delete")
	}

	// Deleting a missing key is S3-idempotent, not an error.
	h.Delete("a.txt")
}

func Test_Handle_List(t *testing.T) {
	h, _ := setupTestHandle(t)

	h.Put("assets/1.png", "a")
	h.Put("assets/sub/2.png", "b")
	h.Put("assets-old/3.png", "c")
	h.Put("other.txt", "d")

	keys := h.List("assets")
	if len(keys) != 2 || keys[0] != "assets/1.png" || keys[1] != "assets/sub/2.png" {
		t.Errorf("List(assets) = %v", keys)
	}

	all := h.List("")
	if len(all) != 4 {
		t.Errorf("List(\"\") = %v, want 4 keys", all)
	}
	for _, key := range all {
		if key == "" || key[0] == '/' {
			t.Errorf("listed key %q is not prefix-relative", key)
		}
	}
}

func Test_Handle_CopyMove(t *testing.T) {
	h, _ := setupTestHandle(t)

	h.Put("src.txt", "payload")
	h.Copy("src.txt", "copies/dst.txt")
	if got := h.Get("copies/dst.txt"); got != "payload" {
		t.Errorf("copied content = %q, want payload", got)
	}
	if !h.Exists("src.txt") {
		t.Error("source should survive Copy")
	}

	h.Move("src.txt", "moved/src.txt")
	if h.Exists("src.txt") {
		t.Error("source should be gone after Move")
	}
	if got := h.Get("moved/src.txt"); got != "payload" {
		t.Errorf("moved content = %q, want payload", got)
	}
}
