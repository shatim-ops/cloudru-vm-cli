// Package s3 is a client for Cloud.ru Object Storage (S3-compatible, SigV4),
// replacing the legacy python boto3 path in cloudru_api.py. Endpoint + keys
// come from the env (CLOUDRU_S3_ENDPOINT / _ACCESS_KEY / _SECRET_KEY / _REGION),
// exported by the MCP wrapper from .cloudru/credentials.json. Uses minio-go,
// which is purpose-built for S3-compatible endpoints like s3.cloud.ru.
package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client wraps a minio S3 client.
type Client struct {
	mc     *minio.Client
	region string
}

// Config holds the S3 connection parameters.
type Config struct {
	Endpoint  string // full URL, e.g. https://s3.cloud.ru
	AccessKey string
	SecretKey string
	Region    string
}

// New builds an S3 client from config. Endpoint may include a scheme; the
// scheme decides TLS (https → Secure).
func New(cfg Config) (*Client, error) {
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("S3 credentials not configured (set CLOUDRU_S3_ACCESS_KEY / CLOUDRU_S3_SECRET_KEY)")
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "https://s3.cloud.ru"
	}
	secure := true
	if strings.HasPrefix(endpoint, "http://") {
		secure = false
		endpoint = strings.TrimPrefix(endpoint, "http://")
	} else {
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}
	endpoint = strings.TrimSuffix(endpoint, "/")
	region := cfg.Region
	if region == "" {
		region = "ru-central-1"
	}
	mc, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("init S3 client: %w", err)
	}
	return &Client{mc: mc, region: region}, nil
}

// Bucket describes a bucket.
type Bucket struct {
	Name    string `json:"name"`
	Created string `json:"created,omitempty"`
}

// ListBuckets lists all buckets.
func (c *Client) ListBuckets(ctx context.Context) ([]Bucket, error) {
	bs, err := c.mc.ListBuckets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list buckets: %w", err)
	}
	out := make([]Bucket, 0, len(bs))
	for _, b := range bs {
		out = append(out, Bucket{Name: b.Name, Created: b.CreationDate.Format(time.RFC3339)})
	}
	return out, nil
}

// Object describes an object entry.
type Object struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	LastModified string `json:"last_modified,omitempty"`
}

// ListObjects lists up to maxKeys objects under prefix.
func (c *Client) ListObjects(ctx context.Context, bucket, prefix string, maxKeys int) ([]Object, error) {
	if maxKeys <= 0 {
		maxKeys = 1000
	}
	opts := minio.ListObjectsOptions{Prefix: prefix, Recursive: true, MaxKeys: maxKeys}
	var out []Object
	for obj := range c.mc.ListObjects(ctx, bucket, opts) {
		if obj.Err != nil {
			return nil, fmt.Errorf("list objects: %w", obj.Err)
		}
		out = append(out, Object{Key: obj.Key, Size: obj.Size, LastModified: obj.LastModified.Format(time.RFC3339)})
		if len(out) >= maxKeys {
			break
		}
	}
	return out, nil
}

// GetObject returns an object's bytes.
func (c *Client) GetObject(ctx context.Context, bucket, key string) ([]byte, error) {
	obj, err := c.mc.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object: %w", err)
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("read object %s/%s: %w", bucket, key, err)
	}
	return data, nil
}

// PutObject uploads bytes to bucket/key.
func (c *Client) PutObject(ctx context.Context, bucket, key string, data []byte, contentType string) (int64, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	info, err := c.mc.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return 0, fmt.Errorf("put object: %w", err)
	}
	return info.Size, nil
}

// Stat returns an object's metadata. The boolean reports existence: a missing
// key is NOT an error here, because the callers use this to decide whether a
// write would overwrite something.
func (c *Client) Stat(ctx context.Context, bucket, key string) (Object, bool, error) {
	info, err := c.mc.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).StatusCode == 404 {
			return Object{}, false, nil
		}
		return Object{}, false, fmt.Errorf("stat object: %w", err)
	}
	return Object{Key: info.Key, Size: info.Size, LastModified: info.LastModified.Format(time.RFC3339)}, true, nil
}

// PutFile uploads a file from disk. minio-go streams it and switches to
// multipart on its own, so this is the only path that can carry a product
// artifact (a RAW image is tens of gigabytes and cannot go through the MCP
// arguments as text or base64).
func (c *Client) PutFile(ctx context.Context, bucket, key, path, contentType string) (int64, string, error) {
	if contentType == "" {
		contentType = contentTypeFor(path)
	}
	info, err := c.mc.FPutObject(ctx, bucket, key, path, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return 0, "", fmt.Errorf("upload %s: %w", path, err)
	}
	return info.Size, info.ETag, nil
}

// PresignGet mints a time-limited download URL. SigV4 caps the lifetime at 7
// days; anything longer is rejected by the service, so it is rejected here with
// a clear message instead.
//
// The URL is a bearer credential: whoever holds it can download the object
// until it expires. It does not belong in git, in a ticket, or in a chat that
// outlives the link.
func (c *Client) PresignGet(ctx context.Context, bucket, key string, expires time.Duration) (string, error) {
	if expires <= 0 {
		expires = 7 * 24 * time.Hour
	}
	if expires > 7*24*time.Hour {
		return "", fmt.Errorf("presigned URLs live at most 7 days (604800s), got %s", expires)
	}
	u, err := c.mc.PresignedGetObject(ctx, bucket, key, expires, nil)
	if err != nil {
		return "", fmt.Errorf("presign %s/%s: %w", bucket, key, err)
	}
	return u.String(), nil
}

// contentTypeFor guesses a MIME type from the file extension. The default is
// deliberately application/octet-stream: a wrong text/* type would make some
// clients mangle a binary artifact.
func contentTypeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "application/json"
	case ".txt", ".md", ".sha256":
		return "text/plain; charset=utf-8"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".tar":
		return "application/x-tar"
	case ".gz", ".tgz":
		return "application/gzip"
	case ".zip":
		return "application/zip"
	case ".svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}

// RemoveObject deletes bucket/key.
func (c *Client) RemoveObject(ctx context.Context, bucket, key string) error {
	if err := c.mc.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove object: %w", err)
	}
	return nil
}

// MakeBucket creates a bucket. region defaults to the client's configured region.
func (c *Client) MakeBucket(ctx context.Context, bucket, region string) error {
	if region == "" {
		region = c.region
	}
	if err := c.mc.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		// idempotent: a bucket that already belongs to us is not an error
		exists, errEx := c.mc.BucketExists(ctx, bucket)
		if errEx == nil && exists {
			return nil
		}
		return fmt.Errorf("create bucket: %w", err)
	}
	return nil
}

// RemoveBucket deletes an EMPTY bucket.
func (c *Client) RemoveBucket(ctx context.Context, bucket string) error {
	if err := c.mc.RemoveBucket(ctx, bucket); err != nil {
		return fmt.Errorf("remove bucket: %w", err)
	}
	return nil
}
