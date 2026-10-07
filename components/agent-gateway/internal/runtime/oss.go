package runtime

import (
	"context"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"io"
	"strconv"
)

// OSSObjects uses the reviewed controller credential, never sandbox auth.
// The caller constructs its client with certificate validation, fixed endpoint,
// redirect refusal, finite timeouts and no credential-bearing debug logging.
type OSSObjects struct {
	Client     *oss.Client
	Bucket     *oss.Bucket
	BucketName string
}

func (o OSSObjects) BucketEncryption(ctx context.Context) (string, error) {
	r, e := o.Client.GetBucketEncryption(o.BucketName, oss.WithContext(ctx))
	if e != nil {
		return "", ErrCheckpointVerification
	}
	return r.SSEDefault.SSEAlgorithm, nil
}
func (o OSSObjects) Head(ctx context.Context, key string) (ObjectMeta, error) {
	h, e := o.Bucket.GetObjectDetailedMeta(key, oss.WithContext(ctx))
	if e != nil {
		return ObjectMeta{}, ErrCheckpointVerification
	}
	size, e := strconv.ParseInt(h.Get("Content-Length"), 10, 64)
	if e != nil {
		return ObjectMeta{}, ErrCheckpointVerification
	}
	return ObjectMeta{Bytes: size, Encryption: h.Get("X-Oss-Server-Side-Encryption")}, nil
}
func (o OSSObjects) Read(ctx context.Context, key string) (io.ReadCloser, error) {
	r, e := o.Bucket.GetObject(key, oss.WithContext(ctx))
	if e != nil {
		return nil, ErrCheckpointVerification
	}
	return r, nil
}
