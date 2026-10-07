package execution

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"sync"
)

type LiveLease struct {
	mu   sync.Mutex
	conn *pgxpool.Conn
}

func (s *Store) AcquireLiveWorker(ctx context.Context) (*LiveLease, error) {
	c, e := s.Pool.Acquire(ctx)
	if e != nil {
		return nil, e
	}
	var held bool
	if e = c.QueryRow(ctx, "SELECT pg_try_advisory_lock(762019001)").Scan(&held); e != nil || !held {
		c.Release()
		return nil, ErrUnavailable
	}
	return &LiveLease{conn: c}, nil
}
func (l *LiveLease) Valid(ctx context.Context) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil || l.conn.Conn().IsClosed() {
		return false
	}
	var held bool
	e := l.conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' AND classid=0 AND objid=762019001 AND granted)").Scan(&held)
	return e == nil && held
}
func (l *LiveLease) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn != nil {
		l.conn.Exec(context.Background(), "SELECT pg_advisory_unlock(762019001)")
		l.conn.Release()
		l.conn = nil
	}
}
