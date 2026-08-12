package s3

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// fakeS3 is an in-memory single-bucket s3API for unit tests, including a real
// multipart-upload implementation so the streamed-write path is exercised.
type fakeS3 struct {
	bucket       string
	objects      map[string][]byte
	uploads      map[string]map[int32][]byte
	nextUploadID int
}

func newFakeS3(bucket string) *fakeS3 {
	return &fakeS3{
		bucket:  bucket,
		objects: map[string][]byte{},
		uploads: map[string]map[int32][]byte{},
	}
}

func (f *fakeS3) checkBucket(bucket *string) error {
	if aws.ToString(bucket) != f.bucket {
		return &types.NoSuchBucket{}
	}
	return nil
}

func (f *fakeS3) HeadBucket(ctx context.Context, params *awss3.HeadBucketInput, optFns ...func(*awss3.Options)) (*awss3.HeadBucketOutput, error) {
	if err := f.checkBucket(params.Bucket); err != nil {
		return nil, err
	}
	return &awss3.HeadBucketOutput{}, nil
}

func (f *fakeS3) GetObject(ctx context.Context, params *awss3.GetObjectInput, optFns ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	if err := f.checkBucket(params.Bucket); err != nil {
		return nil, err
	}
	body, ok := f.objects[aws.ToString(params.Key)]
	if !ok {
		return nil, &types.NoSuchKey{}
	}
	return &awss3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(string(body)))}, nil
}

func (f *fakeS3) PutObject(ctx context.Context, params *awss3.PutObjectInput, optFns ...func(*awss3.Options)) (*awss3.PutObjectOutput, error) {
	if err := f.checkBucket(params.Bucket); err != nil {
		return nil, err
	}
	if aws.ToString(params.IfNoneMatch) == "*" {
		if _, exists := f.objects[aws.ToString(params.Key)]; exists {
			return nil, &smithy.GenericAPIError{Code: "PreconditionFailed", Message: "at least one of the preconditions you specified did not hold"}
		}
	}
	body, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	f.objects[aws.ToString(params.Key)] = body
	return &awss3.PutObjectOutput{}, nil
}

func (f *fakeS3) DeleteObject(ctx context.Context, params *awss3.DeleteObjectInput, optFns ...func(*awss3.Options)) (*awss3.DeleteObjectOutput, error) {
	if err := f.checkBucket(params.Bucket); err != nil {
		return nil, err
	}
	delete(f.objects, aws.ToString(params.Key))
	return &awss3.DeleteObjectOutput{}, nil
}

func (f *fakeS3) HeadObject(ctx context.Context, params *awss3.HeadObjectInput, optFns ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error) {
	if err := f.checkBucket(params.Bucket); err != nil {
		return nil, err
	}
	if _, ok := f.objects[aws.ToString(params.Key)]; !ok {
		return nil, &types.NotFound{}
	}
	return &awss3.HeadObjectOutput{}, nil
}

func (f *fakeS3) CopyObject(ctx context.Context, params *awss3.CopyObjectInput, optFns ...func(*awss3.Options)) (*awss3.CopyObjectOutput, error) {
	if err := f.checkBucket(params.Bucket); err != nil {
		return nil, err
	}
	copySource, err := url.PathUnescape(aws.ToString(params.CopySource))
	if err != nil {
		return nil, err
	}
	bucket, key, ok := strings.Cut(copySource, "/")
	if !ok || bucket != f.bucket {
		return nil, fmt.Errorf("bad CopySource %q", copySource)
	}
	body, exists := f.objects[key]
	if !exists {
		return nil, &types.NoSuchKey{}
	}
	f.objects[aws.ToString(params.Key)] = append([]byte(nil), body...)
	return &awss3.CopyObjectOutput{}, nil
}

func (f *fakeS3) CreateMultipartUpload(ctx context.Context, params *awss3.CreateMultipartUploadInput, optFns ...func(*awss3.Options)) (*awss3.CreateMultipartUploadOutput, error) {
	if err := f.checkBucket(params.Bucket); err != nil {
		return nil, err
	}
	f.nextUploadID++
	uploadID := fmt.Sprintf("upload-%d", f.nextUploadID)
	f.uploads[uploadID] = map[int32][]byte{}
	return &awss3.CreateMultipartUploadOutput{UploadId: aws.String(uploadID)}, nil
}

func (f *fakeS3) UploadPart(ctx context.Context, params *awss3.UploadPartInput, optFns ...func(*awss3.Options)) (*awss3.UploadPartOutput, error) {
	parts, ok := f.uploads[aws.ToString(params.UploadId)]
	if !ok {
		return nil, fmt.Errorf("unknown upload %q", aws.ToString(params.UploadId))
	}
	body, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	parts[aws.ToInt32(params.PartNumber)] = body
	return &awss3.UploadPartOutput{ETag: aws.String(fmt.Sprintf("etag-%d", aws.ToInt32(params.PartNumber)))}, nil
}

func (f *fakeS3) CompleteMultipartUpload(ctx context.Context, params *awss3.CompleteMultipartUploadInput, optFns ...func(*awss3.Options)) (*awss3.CompleteMultipartUploadOutput, error) {
	parts, ok := f.uploads[aws.ToString(params.UploadId)]
	if !ok {
		return nil, fmt.Errorf("unknown upload %q", aws.ToString(params.UploadId))
	}
	var body []byte
	for _, part := range params.MultipartUpload.Parts {
		data, ok := parts[aws.ToInt32(part.PartNumber)]
		if !ok {
			return nil, fmt.Errorf("missing part %d", aws.ToInt32(part.PartNumber))
		}
		body = append(body, data...)
	}
	f.objects[aws.ToString(params.Key)] = body
	delete(f.uploads, aws.ToString(params.UploadId))
	return &awss3.CompleteMultipartUploadOutput{}, nil
}

func (f *fakeS3) AbortMultipartUpload(ctx context.Context, params *awss3.AbortMultipartUploadInput, optFns ...func(*awss3.Options)) (*awss3.AbortMultipartUploadOutput, error) {
	delete(f.uploads, aws.ToString(params.UploadId))
	return &awss3.AbortMultipartUploadOutput{}, nil
}

func (f *fakeS3) ListObjectsV2(ctx context.Context, params *awss3.ListObjectsV2Input, optFns ...func(*awss3.Options)) (*awss3.ListObjectsV2Output, error) {
	if err := f.checkBucket(params.Bucket); err != nil {
		return nil, err
	}
	prefix := aws.ToString(params.Prefix)
	var keys []string
	for key := range f.objects {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	// Pages of one exercise the handle's continuation loop.
	start := 0
	if params.ContinuationToken != nil {
		start = sort.SearchStrings(keys, aws.ToString(params.ContinuationToken))
	}
	out := &awss3.ListObjectsV2Output{}
	if start < len(keys) {
		out.Contents = []types.Object{{Key: aws.String(keys[start])}}
		if start+1 < len(keys) {
			out.NextContinuationToken = aws.String(keys[start+1])
		}
	}
	return out, nil
}
