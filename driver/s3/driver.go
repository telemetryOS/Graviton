// Package s3 is the S3 object-store driver. A configured "database" is a
// bucket (optionally under a key prefix); migration scripts get, put, copy,
// and delete objects in it. S3-compatible stores (MinIO, Cloudflare R2,
// DigitalOcean Spaces, …) work through the endpoint/path-style options, the
// same way MariaDB works with the mysql driver.
//
// connection_url has the shape:
//
//	s3://bucket[/prefix]?region=us-east-1[&endpoint=http://localhost:9000][&path-style=true][&access-key=...][&secret-key=...]
//
// When access-key/secret-key are omitted, credentials come from the standard
// AWS chain (environment, shared config, IAM role). Remember the config file
// templates ${ENV_VARS}, so secrets belong in the environment either way.
//
// Object stores have no transactions. Every handle operation applies
// immediately: BeginTx/CommitTx/RollbackTx are no-ops, so a failed migration
// body does NOT undo writes that already happened. This matches Graviton's
// recovery model — idempotent/convergent migrations plus re-run.
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/telemetryos/graviton/config"
	"github.com/telemetryos/graviton/driver/internal/jsbytes"
	"github.com/telemetryos/graviton/driver/internal/jsontracking"
	migrationsmeta "github.com/telemetryos/graviton/migrations-meta"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/dop251/goja"
)

// MIGRATIONS_OBJECT is the tracking document's key (under the configured
// prefix) when this driver is the migrations_db.
const MIGRATIONS_OBJECT = "graviton-migrations.json"

// s3API is the slice of the S3 client the driver uses; *awss3.Client satisfies
// it and tests substitute an in-memory fake. The multipart operations are
// manager.UploadAPIClient's — the upload manager drives them for streamed
// writes larger than one part.
type s3API interface {
	HeadBucket(ctx context.Context, params *awss3.HeadBucketInput, optFns ...func(*awss3.Options)) (*awss3.HeadBucketOutput, error)
	GetObject(ctx context.Context, params *awss3.GetObjectInput, optFns ...func(*awss3.Options)) (*awss3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *awss3.PutObjectInput, optFns ...func(*awss3.Options)) (*awss3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *awss3.DeleteObjectInput, optFns ...func(*awss3.Options)) (*awss3.DeleteObjectOutput, error)
	HeadObject(ctx context.Context, params *awss3.HeadObjectInput, optFns ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error)
	CopyObject(ctx context.Context, params *awss3.CopyObjectInput, optFns ...func(*awss3.Options)) (*awss3.CopyObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *awss3.ListObjectsV2Input, optFns ...func(*awss3.Options)) (*awss3.ListObjectsV2Output, error)
	UploadPart(ctx context.Context, params *awss3.UploadPartInput, optFns ...func(*awss3.Options)) (*awss3.UploadPartOutput, error)
	CreateMultipartUpload(ctx context.Context, params *awss3.CreateMultipartUploadInput, optFns ...func(*awss3.Options)) (*awss3.CreateMultipartUploadOutput, error)
	CompleteMultipartUpload(ctx context.Context, params *awss3.CompleteMultipartUploadInput, optFns ...func(*awss3.Options)) (*awss3.CompleteMultipartUploadOutput, error)
	AbortMultipartUpload(ctx context.Context, params *awss3.AbortMultipartUploadInput, optFns ...func(*awss3.Options)) (*awss3.AbortMultipartUploadOutput, error)
}

// target is a parsed s3:// connection_url.
type target struct {
	bucket    string
	prefix    string
	region    string
	endpoint  string
	pathStyle bool
	accessKey string
	secretKey string
}

type Driver struct {
	config *config.DatabaseConfig
	target *target
	client s3API
}

// New builds an S3 driver for conf. database_name is unused — the bucket is
// named in the connection_url.
func New(conf *config.DatabaseConfig) *Driver {
	return &Driver{config: conf}
}

func parseTarget(connectionUrl string) (*target, error) {
	u, err := url.Parse(connectionUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse s3 connection_url: %w", err)
	}
	if u.Scheme != "s3" {
		return nil, fmt.Errorf("s3 connection_url must start with s3://, got %q", connectionUrl)
	}
	if u.Host == "" {
		return nil, errors.New("s3 connection_url names no bucket")
	}

	q := u.Query()
	t := &target{
		bucket:    u.Host,
		prefix:    strings.Trim(u.Path, "/"),
		region:    q.Get("region"),
		endpoint:  q.Get("endpoint"),
		pathStyle: q.Get("path-style") == "true",
		accessKey: q.Get("access-key"),
		secretKey: q.Get("secret-key"),
	}
	return t, nil
}

// ValidateConfig statically checks an s3 database's connection_url shape, so
// config mistakes surface before any connection is attempted.
func ValidateConfig(conf *config.DatabaseConfig) error {
	_, err := parseTarget(conf.ConnectionUrl)
	return err
}

func (d *Driver) Connect(ctx context.Context) error {
	t, err := parseTarget(d.config.ConnectionUrl)
	if err != nil {
		return err
	}
	d.target = t

	loadOptions := []func(*awsconfig.LoadOptions) error{}
	if t.region != "" {
		loadOptions = append(loadOptions, awsconfig.WithRegion(t.region))
	}
	if t.accessKey != "" || t.secretKey != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(t.accessKey, t.secretKey, ""),
		))
	}

	awsConf, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	client := awss3.NewFromConfig(awsConf, func(o *awss3.Options) {
		if t.endpoint != "" {
			o.BaseEndpoint = aws.String(t.endpoint)
		}
		if t.pathStyle {
			o.UsePathStyle = true
		}
		if o.Region == "" {
			// S3-compatible stores generally ignore the region, but the SDK
			// requires one to sign requests.
			o.Region = "us-east-1"
		}
	})

	if _, err := client.HeadBucket(ctx, &awss3.HeadBucketInput{Bucket: aws.String(t.bucket)}); err != nil {
		return fmt.Errorf("failed to access s3 bucket %q: %w", t.bucket, err)
	}

	d.client = client
	return nil
}

func (d *Driver) Disconnect(ctx context.Context) error {
	return nil
}

func (d *Driver) GetAppliedMigrationsMetadata(ctx context.Context) ([]*migrationsmeta.MigrationMetadata, error) {
	out, err := d.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(d.target.bucket),
		Key:    aws.String(d.fullKey(MIGRATIONS_OBJECT)),
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
	return jsontracking.Unmarshal(data)
}

func (d *Driver) SetAppliedMigrationsMetadata(ctx context.Context, migrationsMetadata []*migrationsmeta.MigrationMetadata) error {
	data, err := jsontracking.Marshal(migrationsMetadata)
	if err != nil {
		return err
	}
	_, err = d.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:      aws.String(d.target.bucket),
		Key:         aws.String(d.fullKey(MIGRATIONS_OBJECT)),
		Body:        strings.NewReader(string(data)),
		ContentType: aws.String("application/json"),
	})
	return err
}

// BeginTx is a no-op: object stores have no transactions, so handle operations
// apply immediately and are not rolled back on failure.
func (d *Driver) BeginTx(ctx context.Context) error { return nil }

// CommitTx is a no-op; see BeginTx.
func (d *Driver) CommitTx(ctx context.Context) error { return nil }

// RollbackTx is a no-op; see BeginTx.
func (d *Driver) RollbackTx(ctx context.Context) error { return nil }

// HasOpenTx always reports false; see BeginTx.
func (d *Driver) HasOpenTx() bool { return false }

func (d *Driver) Handle(ctx context.Context) any {
	return &Handle{ctx: ctx, driver: d}
}

func (d *Driver) Init(ctx context.Context, runtime *goja.Runtime) {}

func (d *Driver) Globals(ctx context.Context, runtime *goja.Runtime) map[string]any {
	return map[string]any{}
}

func (d *Driver) MaybeFromJSValue(ctx context.Context, jsvm *goja.Runtime, value goja.Value) (any, bool) {
	return nil, false
}

// MaybeIntoJSValue surfaces getBytes() results as ArrayBuffer values.
func (d *Driver) MaybeIntoJSValue(ctx context.Context, jsvm *goja.Runtime, value any) (goja.Value, bool) {
	return jsbytes.MaybeIntoJSValue(jsvm, value)
}

// listKeys returns every full object key under fullPrefix, treated as a
// folder path (a trailing slash is added to a non-empty prefix so "assets"
// never matches "assets-old/…"), following pagination.
func (d *Driver) listKeys(ctx context.Context, fullPrefix string) ([]string, error) {
	if fullPrefix != "" && !strings.HasSuffix(fullPrefix, "/") {
		fullPrefix += "/"
	}

	var keys []string
	var continuationToken *string
	for {
		out, err := d.client.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{
			Bucket:            aws.String(d.target.bucket),
			Prefix:            aws.String(fullPrefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, err
		}
		for _, obj := range out.Contents {
			keys = append(keys, *obj.Key)
		}
		if out.NextContinuationToken == nil {
			return keys, nil
		}
		continuationToken = out.NextContinuationToken
	}
}

// RenameDatabase moves this driver's configured key prefix to newName (a
// literal prefix in the same bucket) — the retire-databases pattern for
// object stores. S3 has no rename, so it server-side copies every object to
// the new prefix and then deletes the sources. It refuses to run on a
// database without a key prefix (a whole bucket cannot be renamed) and
// refuses a target prefix that already has objects.
//
// Like the mongodb rename, it is immediate and non-transactional — and being
// a mass copy it is also not atomic: a failure mid-way leaves some objects
// copied and none deleted (re-running converges). Keep it in a dedicated
// retire-databases migration.
func (d *Driver) RenameDatabase(ctx context.Context, newName string) error {
	if d.target.prefix == "" {
		return fmt.Errorf(
			"cannot rename s3 database %q: it has no key prefix, and a whole bucket cannot be renamed",
			d.config.Name,
		)
	}

	newPrefix := strings.Trim(newName, "/")
	if newPrefix == "" {
		return errors.New("cannot rename database to an empty prefix")
	}
	if newPrefix == d.target.prefix {
		return fmt.Errorf("cannot rename database prefix %q to itself", newPrefix)
	}

	existing, err := d.listKeys(ctx, newPrefix)
	if err != nil {
		return err
	}
	if len(existing) != 0 {
		return fmt.Errorf("rename target prefix %q already has %d objects, refusing to overwrite it", newPrefix, len(existing))
	}

	sourceKeys, err := d.listKeys(ctx, d.target.prefix)
	if err != nil {
		return err
	}

	for _, sourceKey := range sourceKeys {
		targetKey := newPrefix + "/" + strings.TrimPrefix(sourceKey, d.target.prefix+"/")
		_, err := d.client.CopyObject(ctx, &awss3.CopyObjectInput{
			Bucket:     aws.String(d.target.bucket),
			Key:        aws.String(targetKey),
			CopySource: aws.String(escapeCopySource(d.target.bucket + "/" + sourceKey)),
		})
		if err != nil {
			return fmt.Errorf("failed to copy %q to %q: %w", sourceKey, targetKey, err)
		}
	}

	// Delete sources only after every copy has succeeded.
	for _, sourceKey := range sourceKeys {
		_, err := d.client.DeleteObject(ctx, &awss3.DeleteObjectInput{
			Bucket: aws.String(d.target.bucket),
			Key:    aws.String(sourceKey),
		})
		if err != nil {
			return fmt.Errorf("failed to delete %q after copy: %w", sourceKey, err)
		}
	}

	return nil
}

// fullKey maps a script-supplied key onto the configured prefix.
func (d *Driver) fullKey(key string) string {
	key = strings.TrimPrefix(key, "/")
	if d.target.prefix == "" {
		return key
	}
	if key == "" {
		return d.target.prefix
	}
	return d.target.prefix + "/" + key
}

// relativeKey strips the configured prefix from a listed object key, so list()
// results address objects the same way get()/put() do.
func (d *Driver) relativeKey(fullKey string) string {
	if d.target.prefix == "" {
		return fullKey
	}
	return strings.TrimPrefix(fullKey, d.target.prefix+"/")
}
