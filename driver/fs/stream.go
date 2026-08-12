package fs

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

// OpenRead opens a streaming reader over one file.
func (d *Driver) OpenRead(ctx context.Context, path string) (io.ReadCloser, error) {
	resolved, err := d.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.Open(resolved)
}

// WriteStream writes r to the file, creating parent directories as needed. It
// copies in bounded memory, so arbitrarily large streams never buffer whole; a
// failed copy removes the partial file rather than leaving a truncated one.
func (d *Driver) WriteStream(ctx context.Context, path string, r io.Reader) error {
	resolved, err := d.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return err
	}

	file, err := os.Create(resolved)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, r); err != nil {
		file.Close()
		os.Remove(resolved)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(resolved)
		return err
	}
	return nil
}
