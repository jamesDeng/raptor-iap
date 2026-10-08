package store

import (
	"context"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/config"
	"io"
	"time"
)

type Object struct {
	Body                io.ReadCloser
	Version, Encryption string
}
type ObjectReader interface {
	Get(context.Context, config.Reference) (Object, error)
}
type Store struct{ Reader ObjectReader }

func (s Store) Fetch(ctx context.Context, ref config.Reference) ([]byte, error) {
	if s.Reader == nil {
		return nil, errors.New("retrieval_unavailable")
	}
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return nil, errors.New("retrieval_cancelled")
		}
		obj, e := s.Reader.Get(ctx, ref)
		if e != nil {
			if obj.Body != nil {
				obj.Body.Close()
			}
			if attempt < 2 {
				timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, errors.New("retrieval_cancelled")
				case <-timer.C:
				}
				continue
			}
			return nil, errors.New("retrieval_failed")
		}
		if obj.Body == nil {
			return nil, errors.New("retrieval_invalid_response")
		}
		if obj.Version != ref.Version || obj.Encryption != "AES256" {
			obj.Body.Close()
			return nil, errors.New("retrieval_metadata_mismatch")
		}
		b, e := io.ReadAll(io.LimitReader(obj.Body, config.MaxBundle+1))
		obj.Body.Close()
		if e != nil {
			return nil, errors.New("retrieval_read_failed")
		}
		if len(b) > config.MaxBundle {
			return nil, errors.New("retrieval_oversized")
		}
		return b, nil
	}
	return nil, errors.New("retrieval_failed")
}
