package s3

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/telemetryos/graviton/driver/internal/lockretry"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// MIGRATIONS_LOCK_OBJECT is the whole-run migrations lock, a sibling of the
// tracking object when this driver is the migrations_db.
const MIGRATIONS_LOCK_OBJECT = "graviton-migrations.lock.json"

// AcquireMigrationsLock claims the lock with a conditional put
// (If-None-Match: *), which atomically fails with 412 PreconditionFailed when
// the lock object already exists. Conditional writes are supported by AWS S3
// and current S3-compatible stores (MinIO, R2).
func (d *Driver) AcquireMigrationsLock(ctx context.Context, lock *migrationsmeta.MigrationsLock) (*migrationsmeta.MigrationsLock, error) {
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return nil, err
	}

	return lockretry.Acquire(
		func() (bool, error) {
			_, err := d.client.PutObject(ctx, &awss3.PutObjectInput{
				Bucket:      aws.String(d.target.bucket),
				Key:         aws.String(d.fullKey(MIGRATIONS_LOCK_OBJECT)),
				Body:        strings.NewReader(string(data)),
				ContentType: aws.String("application/json"),
				IfNoneMatch: aws.String("*"),
			})
			if err != nil {
				var apiErr smithy.APIError
				if errors.As(err, &apiErr) && apiErr.ErrorCode() == "PreconditionFailed" {
					return false, nil
				}
				return false, err
			}
			return true, nil
		},
		func() (*migrationsmeta.MigrationsLock, error) {
			return d.GetMigrationsLock(ctx)
		},
	)
}

func (d *Driver) ReleaseMigrationsLock(ctx context.Context, holder string) error {
	current, err := d.GetMigrationsLock(ctx)
	if err != nil {
		return err
	}
	if current == nil || current.Holder != holder {
		return nil
	}
	return d.ClearMigrationsLock(ctx)
}

func (d *Driver) GetMigrationsLock(ctx context.Context) (*migrationsmeta.MigrationsLock, error) {
	out, err := d.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(d.target.bucket),
		Key:    aws.String(d.fullKey(MIGRATIONS_LOCK_OBJECT)),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, nil
		}
		return nil, err
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, err
	}

	var lock migrationsmeta.MigrationsLock
	if err := json.Unmarshal(data, &lock); err != nil {
		// A foreign lock object still means the lock is held; report it with
		// what little is known so unlock can clear it.
		return &migrationsmeta.MigrationsLock{Hostname: "unknown"}, nil
	}
	return &lock, nil
}

func (d *Driver) ClearMigrationsLock(ctx context.Context) error {
	_, err := d.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(d.target.bucket),
		Key:    aws.String(d.fullKey(MIGRATIONS_LOCK_OBJECT)),
	})
	return err
}
