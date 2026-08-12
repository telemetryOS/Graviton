package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestHandle(t *testing.T) (*Handle, *Driver) {
	t.Helper()
	drv, ctx := setupTestDriver(t)
	return drv.Handle(ctx).(*Handle), drv
}

// expectPanic runs fn and fails the test unless it panics.
func expectPanic(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s should panic", what)
		}
	}()
	fn()
}

func Test_Handle_WriteRead(t *testing.T) {
	h, _ := setupTestHandle(t)

	h.Write("docs/readme.txt", "hello")
	if got := h.Read("docs/readme.txt"); got != "hello" {
		t.Errorf("Read() = %q, want %q", got, "hello")
	}

	h.Write("bin/data", []byte{0x00, 0xff, 0x10})
	got := h.ReadBytes("bin/data")
	if len(got) != 3 || got[0] != 0x00 || got[1] != 0xff || got[2] != 0x10 {
		t.Errorf("ReadBytes() = %v, want [0 255 16]", got)
	}
}

func Test_Handle_Write_RejectsBadBody(t *testing.T) {
	h, _ := setupTestHandle(t)
	expectPanic(t, "write() with a number body", func() {
		h.Write("x.txt", 42)
	})
}

func Test_Handle_ExistsRemove(t *testing.T) {
	h, _ := setupTestHandle(t)

	if h.Exists("missing.txt") {
		t.Error("Exists(missing) = true, want false")
	}

	h.Write("a.txt", "x")
	if !h.Exists("a.txt") {
		t.Error("Exists(a.txt) = false, want true")
	}

	h.Remove("a.txt")
	if h.Exists("a.txt") {
		t.Error("Exists(a.txt) = true after Remove")
	}

	expectPanic(t, "remove() of a missing file", func() {
		h.Remove("missing.txt")
	})
}

func Test_Handle_RemoveAll(t *testing.T) {
	h, drv := setupTestHandle(t)

	h.Write("tree/a/b.txt", "x")
	h.RemoveAll("tree")
	if h.Exists("tree") {
		t.Error("tree still exists after RemoveAll")
	}

	// Missing paths are fine.
	h.RemoveAll("tree")

	expectPanic(t, "removeAll() of the root", func() {
		h.RemoveAll(".")
	})
	if _, err := os.Stat(drv.root); err != nil {
		t.Fatalf("root should survive: %v", err)
	}
}

func Test_Handle_MkdirList(t *testing.T) {
	h, _ := setupTestHandle(t)

	h.Mkdir("sub/dir")
	h.Write("sub/b.txt", "bb")
	h.Write("sub/a.txt", "a")

	entries := h.List("sub")
	if len(entries) != 3 {
		t.Fatalf("List() returned %d entries, want 3", len(entries))
	}
	if entries[0].Name != "a.txt" || entries[0].IsDir || entries[0].Size != 1 {
		t.Errorf("entries[0] = %+v, want a.txt file of size 1", entries[0])
	}
	if entries[1].Name != "b.txt" || entries[1].Size != 2 {
		t.Errorf("entries[1] = %+v, want b.txt file of size 2", entries[1])
	}
	if entries[2].Name != "dir" || !entries[2].IsDir {
		t.Errorf("entries[2] = %+v, want dir directory", entries[2])
	}
}

func Test_Handle_CopyMove(t *testing.T) {
	h, _ := setupTestHandle(t)

	h.Write("src.txt", "payload")
	h.Copy("src.txt", "copies/dst.txt")
	if got := h.Read("copies/dst.txt"); got != "payload" {
		t.Errorf("copied content = %q, want %q", got, "payload")
	}
	if !h.Exists("src.txt") {
		t.Error("source should survive Copy")
	}

	h.Move("src.txt", "moved/src.txt")
	if h.Exists("src.txt") {
		t.Error("source should be gone after Move")
	}
	if got := h.Read("moved/src.txt"); got != "payload" {
		t.Errorf("moved content = %q, want %q", got, "payload")
	}
}

func Test_Handle_PathEscapePanics(t *testing.T) {
	h, drv := setupTestHandle(t)

	outside := filepath.Join(filepath.Dir(drv.root), "escape.txt")
	expectPanic(t, "write() outside the root", func() {
		h.Write("../escape.txt", "x")
	})
	if _, err := os.Stat(outside); err == nil {
		t.Error("write escaped the root")
	}
}
