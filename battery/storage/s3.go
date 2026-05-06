package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"
)

// S3Client is a minimal interface for S3-compatible operations.
// This avoids importing the AWS SDK directly; callers provide their own
// implementation or use the PresignedURL field for direct browser uploads.
type S3Client interface {
	PutObject(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType string) error
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	HeadObject(ctx context.Context, bucket, key string) (bool, error)
}

// Presigner generates presigned URLs for direct browser uploads/downloads.
type Presigner interface {
	PresignGet(ctx context.Context, bucket, key string, expires time.Duration) (*url.URL, error)
	PresignPut(ctx context.Context, bucket, key string, expires time.Duration) (*url.URL, error)
}

// S3Option configures an S3Storage instance.
type S3Option func(*S3Storage)

// WithS3Client sets the S3 client implementation.
func WithS3Client(client S3Client) S3Option {
	return func(s *S3Storage) {
		s.Client = client
	}
}

// WithS3Endpoint sets a custom S3-compatible endpoint.
func WithS3Endpoint(endpoint string) S3Option {
	return func(s *S3Storage) {
		s.Endpoint = endpoint
	}
}

// WithPresigner sets the URL presigner for generating presigned URLs.
func WithPresigner(p Presigner) S3Option {
	return func(s *S3Storage) {
		s.presigner = p
	}
}

// S3Storage implements Storage backed by an S3-compatible object store.
// It uses a minimal S3Client interface so no AWS SDK is imported directly.
type S3Storage struct {
	Bucket   string
	Region   string
	Endpoint string
	Client   S3Client
	presigner Presigner
}

// NewS3Storage creates a new S3Storage for the given bucket and region.
// Use WithS3Client to inject an actual client before calling Save/Get/etc.
func NewS3Storage(bucket, region string, opts ...S3Option) *S3Storage {
	s := &S3Storage{
		Bucket: bucket,
		Region: region,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Save stores the contents of r as an S3 object with the given key.
func (s *S3Storage) Save(ctx context.Context, key string, r io.Reader) error {
	if s.Client == nil {
		return fmt.Errorf("storage: s3 client not configured")
	}
	if key == "" {
		return fmt.Errorf("storage: empty key")
	}
	return s.Client.PutObject(ctx, s.Bucket, key, r, -1, "")
}

// Delete removes the S3 object identified by key.
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	if s.Client == nil {
		return fmt.Errorf("storage: s3 client not configured")
	}
	return s.Client.DeleteObject(ctx, s.Bucket, key)
}

// Get returns a ReadCloser for the S3 object identified by key.
func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("storage: s3 client not configured")
	}
	return s.Client.GetObject(ctx, s.Bucket, key)
}

// Exists reports whether an S3 object exists for the given key.
func (s *S3Storage) Exists(ctx context.Context, key string) (bool, error) {
	if s.Client == nil {
		return false, fmt.Errorf("storage: s3 client not configured")
	}
	return s.Client.HeadObject(ctx, s.Bucket, key)
}

// PresignedGetURL returns a presigned URL for downloading the object.
func (s *S3Storage) PresignedGetURL(ctx context.Context, key string, expires time.Duration) (*url.URL, error) {
	if s.presigner == nil {
		return nil, fmt.Errorf("storage: presigner not configured")
	}
	return s.presigner.PresignGet(ctx, s.Bucket, key, expires)
}

// PresignedPutURL returns a presigned URL for uploading the object directly.
func (s *S3Storage) PresignedPutURL(ctx context.Context, key string, expires time.Duration) (*url.URL, error) {
	if s.presigner == nil {
		return nil, fmt.Errorf("storage: presigner not configured")
	}
	return s.presigner.PresignPut(ctx, s.Bucket, key, expires)
}
