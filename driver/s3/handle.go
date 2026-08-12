package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/telemetryos/graviton/driver/internal/jsbytes"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Handle is the migration-facing handle bound to a single s3 database (a
// bucket, optionally under a prefix). Keys are relative to that prefix;
// operations panic on error like every other driver's handle. Operations apply
// immediately — there is no transaction to join.
type Handle struct {
	ctx    context.Context
	driver *Driver
}

// Get returns the object's content as a string (text objects).
func (h *Handle) Get(key string) string {
	return string(h.GetBytes(key))
}

// GetBytes returns the object's raw content; scripts see it as an ArrayBuffer
// (binary-safe, e.g. for copying into an fs database).
func (h *Handle) GetBytes(key string) []byte {
	out, err := h.driver.client.GetObject(h.ctx, &awss3.GetObjectInput{
		Bucket: aws.String(h.driver.target.bucket),
		Key:    aws.String(h.driver.fullKey(key)),
	})
	if err != nil {
		panic(err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		panic(err)
	}
	return data
}

// Put writes data (a string or ArrayBuffer) to the object, replacing any
// existing content.
func (h *Handle) Put(key string, data any) {
	body, ok := jsbytes.ToBytes(data)
	if !ok {
		panic(fmt.Errorf("put() expects a string or ArrayBuffer body, got %T", data))
	}
	_, err := h.driver.client.PutObject(h.ctx, &awss3.PutObjectInput{
		Bucket: aws.String(h.driver.target.bucket),
		Key:    aws.String(h.driver.fullKey(key)),
		Body:   strings.NewReader(string(body)),
	})
	if err != nil {
		panic(err)
	}
}

// Delete removes the object; deleting a missing key is not an error (S3
// semantics).
func (h *Handle) Delete(key string) {
	_, err := h.driver.client.DeleteObject(h.ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(h.driver.target.bucket),
		Key:    aws.String(h.driver.fullKey(key)),
	})
	if err != nil {
		panic(err)
	}
}

// List returns every object key under the given key prefix ("" lists the whole
// database), relative to the configured prefix, following pagination.
func (h *Handle) List(prefix string) []string {
	// A prefix is treated as a folder path: "assets" lists assets/…, not
	// assets-old/…. The same applies to the configured database prefix when
	// listing everything with "".
	fullKeys, err := h.driver.listKeys(h.ctx, h.driver.fullKey(prefix))
	if err != nil {
		panic(err)
	}

	keys := make([]string, 0, len(fullKeys))
	for _, fullKey := range fullKeys {
		keys = append(keys, h.driver.relativeKey(fullKey))
	}
	return keys
}

// Exists reports whether an object exists at key.
func (h *Handle) Exists(key string) bool {
	_, err := h.driver.client.HeadObject(h.ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(h.driver.target.bucket),
		Key:    aws.String(h.driver.fullKey(key)),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return false
		}
		panic(err)
	}
	return true
}

// Copy server-side copies an object within the database.
func (h *Handle) Copy(src string, dst string) {
	_, err := h.driver.client.CopyObject(h.ctx, &awss3.CopyObjectInput{
		Bucket:     aws.String(h.driver.target.bucket),
		Key:        aws.String(h.driver.fullKey(dst)),
		CopySource: aws.String(escapeCopySource(h.driver.target.bucket + "/" + h.driver.fullKey(src))),
	})
	if err != nil {
		panic(err)
	}
}

// escapeCopySource URL-encodes a CopySource ("bucket/key") per path segment.
// The bucket/key separators must stay literal slashes — escaping the whole
// string would turn them into %2F, which not every S3-compatible store
// accepts — while segments themselves need encoding for keys with reserved
// characters.
func escapeCopySource(copySource string) string {
	segments := strings.Split(copySource, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

// Move copies the object then deletes the source.
func (h *Handle) Move(src string, dst string) {
	h.Copy(src, dst)
	h.Delete(src)
}
