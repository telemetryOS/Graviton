package s3

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	// feature/s3/manager is marked deprecated in favor of the pre-1.0
	// feature/s3/transfermanager; move over once that module has a stable API.
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// OpenRead opens a streaming reader over one object; GetObject bodies stream
// from the server, so large objects are never buffered whole.
func (d *Driver) OpenRead(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := d.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(d.target.bucket),
		Key:    aws.String(d.fullKey(key)),
	})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

// WriteStream writes r to the object through the upload manager, which
// buffers one part at a time and switches to a multipart upload when the body
// outgrows a single part — so arbitrarily large streams upload in bounded
// memory. A failed multipart upload is aborted by the manager, leaving no
// partial object behind.
func (d *Driver) WriteStream(ctx context.Context, key string, r io.Reader) error {
	uploader := manager.NewUploader(d.client)
	_, err := uploader.Upload(ctx, &awss3.PutObjectInput{
		Bucket: aws.String(d.target.bucket),
		Key:    aws.String(d.fullKey(key)),
		Body:   r,
	})
	return err
}
