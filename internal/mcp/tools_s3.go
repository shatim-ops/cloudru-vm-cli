package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
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
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("s3_put will upload %d bytes to s3://%s/%s, overwriting the object if it exists. Call again with confirmed=true after the captain approves.", len(data), bucket, key),
		}), true
	}
	cli, err := s.s3Client()
	if err != nil {
		return errResult(err), true
	}
	n, err := cli.PutObject(ctx, bucket, key, data, stringArg(args, "content_type"))
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "ok", "bucket": bucket, "key": key, "size": n}), false
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
