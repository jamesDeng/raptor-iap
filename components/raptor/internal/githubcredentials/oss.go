package githubcredentials

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

type ossBucket interface {
	PutObject(string, io.Reader, ...oss.Option) error
	GetObject(string, ...oss.Option) (io.ReadCloser, error)
	DeleteObject(string, ...oss.Option) error
}

// OSSSecrets keeps PAT objects under one dedicated prefix in a private bucket.
type OSSSecrets struct{ Bucket ossBucket }

var patObjectKey = regexp.MustCompile(`^github-pat/[a-f0-9]{1,64}$`)

func validObjectKey(key string) bool { return patObjectKey.MatchString(key) }

func (s OSSSecrets) Put(ctx context.Context, key string, secret []byte) error {
	if s.Bucket == nil || !validObjectKey(key) || len(secret) == 0 || len(secret) > 4096 {
		return ErrInvalid
	}
	if err := s.Bucket.PutObject(key, bytes.NewReader(secret), oss.WithContext(ctx), oss.ServerSideEncryption("AES256")); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s OSSSecrets) Get(ctx context.Context, key string) ([]byte, error) {
	if s.Bucket == nil || !validObjectKey(key) {
		return nil, ErrInvalid
	}
	r, err := s.Bucket.GetObject(key, oss.WithContext(ctx))
	if err != nil {
		return nil, ErrUnavailable
	}
	defer r.Close()
	secret, err := io.ReadAll(io.LimitReader(r, 4097))
	if err != nil || len(secret) == 0 || len(secret) > 4096 {
		return nil, ErrUnavailable
	}
	return secret, nil
}

func (s OSSSecrets) Delete(ctx context.Context, key string) error {
	if s.Bucket == nil || !validObjectKey(key) {
		return ErrInvalid
	}
	if err := s.Bucket.DeleteObject(key, oss.WithContext(ctx)); err != nil && !errors.Is(err, context.Canceled) {
		return ErrUnavailable
	} else if err != nil {
		return err
	}
	return nil
}
