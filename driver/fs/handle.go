package fs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/telemetryos/graviton/driver/internal/jsbytes"
)

// Handle is the migration-facing handle bound to a single fs database (a root
// directory). All paths are slash-separated and relative to that root;
// operations panic on error like every other driver's handle (the runner
// converts panics into migration failures). Operations apply immediately —
// there is no transaction to join.
type Handle struct {
	ctx    context.Context
	driver *Driver
}

// Entry is one list() result.
type Entry struct {
	Name  string
	IsDir bool
	Size  int64
}

// Read returns the file's content as a string (text files).
func (h *Handle) Read(path string) string {
	return string(h.ReadBytes(path))
}

// ReadBytes returns the file's raw content; scripts see it as an ArrayBuffer
// (binary-safe, e.g. for copying into an s3 database).
func (h *Handle) ReadBytes(path string) []byte {
	data, err := os.ReadFile(h.resolve(path))
	if err != nil {
		panic(err)
	}
	return data
}

// Write writes data (a string or ArrayBuffer) to the file, creating parent
// directories as needed and truncating an existing file.
func (h *Handle) Write(path string, data any) {
	body, ok := jsbytes.ToBytes(data)
	if !ok {
		panic(fmt.Errorf("write() expects a string or ArrayBuffer body, got %T", data))
	}
	target := h.resolve(path)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(target, body, 0644); err != nil {
		panic(err)
	}
}

// Remove removes a file or empty directory.
func (h *Handle) Remove(path string) {
	if err := os.Remove(h.resolve(path)); err != nil {
		panic(err)
	}
}

// RemoveAll removes a file or directory tree; missing paths are not an error.
func (h *Handle) RemoveAll(path string) {
	target := h.resolve(path)
	if target == h.driver.root {
		panic(fmt.Errorf("refusing to removeAll() the root of fs database %q", h.driver.config.Name))
	}
	if err := os.RemoveAll(target); err != nil {
		panic(err)
	}
}

// Mkdir creates a directory and any missing parents.
func (h *Handle) Mkdir(path string) {
	if err := os.MkdirAll(h.resolve(path), 0755); err != nil {
		panic(err)
	}
}

// List returns the direct entries of a directory, sorted by name. "" or "."
// lists the root.
func (h *Handle) List(path string) []*Entry {
	dirEntries, err := os.ReadDir(h.resolve(path))
	if err != nil {
		panic(err)
	}

	entries := make([]*Entry, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		entry := &Entry{Name: dirEntry.Name(), IsDir: dirEntry.IsDir()}
		if !entry.IsDir {
			info, err := dirEntry.Info()
			if err != nil {
				panic(err)
			}
			entry.Size = info.Size()
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// Exists reports whether a file or directory exists at path.
func (h *Handle) Exists(path string) bool {
	_, err := os.Stat(h.resolve(path))
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		panic(err)
	}
	return true
}

// Copy copies a single file, creating the destination's parent directories.
func (h *Handle) Copy(src string, dst string) {
	srcFile, err := os.Open(h.resolve(src))
	if err != nil {
		panic(err)
	}
	defer srcFile.Close()

	target := h.resolve(dst)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		panic(err)
	}
	dstFile, err := os.Create(target)
	if err != nil {
		panic(err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		panic(err)
	}
	if err := dstFile.Close(); err != nil {
		panic(err)
	}
}

// Move renames a file or directory, creating the destination's parent
// directories.
func (h *Handle) Move(src string, dst string) {
	target := h.resolve(dst)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		panic(err)
	}
	if err := os.Rename(h.resolve(src), target); err != nil {
		panic(err)
	}
}

func (h *Handle) resolve(path string) string {
	resolved, err := h.driver.resolve(path)
	if err != nil {
		panic(err)
	}
	return resolved
}
