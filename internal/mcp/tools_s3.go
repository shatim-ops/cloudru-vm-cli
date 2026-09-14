package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"time"
	"unicode/utf8"

	"github.com/shatim-ops/cloudru-vm-cli/internal/s3"
)

// s3Client builds an S3 client from the env vars exported by the MCP wrapper
// (CLOUDRU_S3_ENDPOINT / _ACCESS_KEY / _SECRET_KEY / _REGION).
func (s *Server) s3Client() (*s3.Client, error) {
	return s3.New(s3.Config{
		Endpoint:  os.Getenv("CLOUDRU_S3_ENDPOINT"),
		AccessKey: os.Getenv("CLOUDRU_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("CLOUDRU_S3_SECRET_KEY"),
		Region:    os.Getenv("CLOUDRU_S3_REGION"),
	})
}

func (s *Server) handleS3Buckets(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	bs, err := cli.ListBuckets(ctx)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"buckets": bs}), false
}

func (s *Server) handleS3Ls(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	if bucket == "" {
		return errResult(fmt.Errorf("'bucket' is required")), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	objs, err := cli.ListObjects(ctx, bucket, stringArg(args, "prefix"), pgInt(args, "max_keys", 1000))
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"bucket": bucket, "count": len(objs), "objects": objs}), false
}

func (s *Server) handleS3Get(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	key := stringArg(args, "key")
	if bucket == "" || key == "" {
		return errResult(fmt.Errorf("'bucket' and 'key' are required")), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	data, err := cli.GetObject(ctx, bucket, key)
	if err != nil {
		return errResult(err), true
	}
	out := map[string]any{"bucket": bucket, "key": key, "size": len(data)}
	// Return text inline when it's valid UTF-8 and not too large; otherwise base64.
	if utf8.Valid(data) && len(data) <= 262144 {
		out["encoding"] = "utf-8"
		out["content"] = string(data)
	} else {
		out["encoding"] = "base64"
		out["content_base64"] = base64.StdEncoding.EncodeToString(data)
	}
	return toJSON(out), false
}

func (s *Server) handleS3Put(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	key := stringArg(args, "key")
	if bucket == "" || key == "" {
		return errResult(fmt.Errorf("'bucket' and 'key' are required")), true
	}
	// Body from inline text (content) or base64 (content_base64).
	var data []byte
	if b64 := stringArg(args, "content_base64"); b64 != "" {
		d, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return errResult(fmt.Errorf("invalid content_base64: %w", err)), true
		}
		data = d
	} else {
		data = []byte(stringArg(args, "content"))
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	if blocked, res := refuseOverwrite(ctx, cli, args, bucket, key); blocked {
		return res, true
	}
	n, err := cli.PutObject(ctx, bucket, key, data, stringArg(args, "content_type"))
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "ok", "bucket": bucket, "key": key, "size": n}), false
}

// refuseOverwrite stops a write that would silently replace an existing object.
//
// Writing a NEW key is cheap and reversible, so it needs no confirmation gate —
// but replacing one destroys whatever was there, and the caller usually does
// not know it is doing so. A published artifact overwritten in place is the bad
// case: the presigned link a partner already holds starts serving different
// bytes under the same sha256 in our handover document.
func refuseOverwrite(ctx context.Context, cli interface {
	Stat(ctx context.Context, bucket, key string) (s3.Object, bool, error)
}, args map[string]any, bucket, key string) (bool, string) {
	if boolArg(args, "overwrite", false) {
		return false, ""
	}
	obj, exists, err := cli.Stat(ctx, bucket, key)
	if err != nil {
		return true, errResult(err)
	}
	if !exists {
		return false, ""
	}
	return true, toJSON(map[string]any{
		"status": "exists",
		"error": fmt.Sprintf("s3://%s/%s already exists (%d bytes, modified %s) — refusing to replace it. "+
			"Pass overwrite=true to replace it deliberately, or write a new key.", bucket, key, obj.Size, obj.LastModified),
		"bucket": bucket, "key": key, "size": obj.Size, "last_modified": obj.LastModified,
	})
}

// handleS3PutFile uploads a file from the engine host's disk. This is the only
// upload path for a product artifact: a RAW VM image is tens of gigabytes and
// cannot pass through the MCP arguments, and minio-go switches to multipart on
// its own here.
func (s *Server) handleS3PutFile(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	key := stringArg(args, "key")
	path := stringArg(args, "path")
	if bucket == "" || key == "" || path == "" {
		return errResult(fmt.Errorf("'bucket', 'key' and 'path' are required")), true
	}
	fi, err := os.Stat(path)
	if err != nil {
		return errResult(fmt.Errorf("cannot read %s: %w", path, err)), true
	}
	if fi.IsDir() {
		return errResult(fmt.Errorf("%s is a directory — pass a single file (pack it first)", path)), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	if blocked, res := refuseOverwrite(ctx, cli, args, bucket, key); blocked {
		return res, true
	}
	n, etag, err := cli.PutFile(ctx, bucket, key, path, stringArg(args, "content_type"))
	if err != nil {
		return errResult(err), true
	}
	out := map[string]any{"status": "ok", "bucket": bucket, "key": key, "size": n, "etag": etag, "path": path}
	if n != fi.Size() {
		// Not fatal — but a silent size change is exactly the kind of thing that
		// turns into "the partner downloaded a truncated image" three days later.
		out["warning"] = fmt.Sprintf("uploaded %d bytes, file on disk is %d", n, fi.Size())
	}
	return toJSON(out), false
}

// handleS3Presign mints a time-limited download URL for an object.
//
// Ungated on purpose: it discloses no more than s3_get, which already returns
// the bytes. But the URL is a bearer credential — see the tool description.
func (s *Server) handleS3Presign(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	key := stringArg(args, "key")
	if bucket == "" || key == "" {
		return errResult(fmt.Errorf("'bucket' and 'key' are required")), true
	}
	secs := pgInt(args, "expires_seconds", 604800) // 7 days, the SigV4 maximum
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	// A link to a key that does not exist is a 404 the recipient discovers for
	// us. Check first, and say which key is missing.
	obj, exists, err := cli.Stat(ctx, bucket, key)
	if err != nil {
		return errResult(err), true
	}
	if !exists {
		return errResult(fmt.Errorf("s3://%s/%s does not exist — nothing to link to", bucket, key)), true
	}
	expires := time.Duration(secs) * time.Second
	url, err := cli.PresignGet(ctx, bucket, key, expires)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{
		"status": "ok", "bucket": bucket, "key": key, "size": obj.Size,
		"url": url, "expires_seconds": secs,
		"expires_at": time.Now().UTC().Add(expires).Format(time.RFC3339),
		"note": "This URL is a temporary download key: anyone holding it can fetch the object until it expires. " +
			"Do not commit it. The signature covers the method, so a HEAD (curl -I) returns 403 — verify with a range GET (curl -r 0-1023), which returns 206.",
	}), false
}

func (s *Server) handleS3Rm(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	key := stringArg(args, "key")
	if bucket == "" || key == "" {
		return errResult(fmt.Errorf("'bucket' and 'key' are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("s3_rm permanently deletes s3://%s/%s. Call again with confirmed=true after the captain approves.", bucket, key),
		}), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	if err := cli.RemoveObject(ctx, bucket, key); err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "deleted", "bucket": bucket, "key": key}), false
}

func (s *Server) handleS3BucketCreate(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	if bucket == "" {
		return errResult(fmt.Errorf("'bucket' is required (the new bucket name)")), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	if err := cli.MakeBucket(ctx, bucket, stringArg(args, "region")); err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "created", "bucket": bucket}), false
}

func (s *Server) handleS3BucketDelete(ctx context.Context, args map[string]any) (string, bool) {
	bucket := stringArg(args, "bucket")
	if bucket == "" {
		return errResult(fmt.Errorf("'bucket' is required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("s3_bucket_delete permanently deletes bucket %q (it must be EMPTY). Call again with confirmed=true after the captain approves.", bucket),
		}), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	if err := cli.RemoveBucket(ctx, bucket); err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "deleted", "bucket": bucket}), false
}
