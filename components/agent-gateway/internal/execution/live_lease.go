package execution

import (
	"context"
	"github.com/jackc/pgx/v5"
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
	lease := &LiveLease{conn: c}
	s.liveLease = lease
	return lease, nil
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

// Lease-backed transactions cannot outlive the session holding the driver lock.
type leaseTx struct {
	pgx.Tx
	once   sync.Once
	unlock func()
}

func (t *leaseTx) Commit(ctx context.Context) error {
	e := t.Tx.Commit(ctx)
	t.once.Do(t.unlock)
	return e
}
func (t *leaseTx) Rollback(ctx context.Context) error {
	e := t.Tx.Rollback(ctx)
	t.once.Do(t.unlock)
	return e
}
func (l *LiveLease) begin(ctx context.Context) (pgx.Tx, error) {
	l.mu.Lock()
	if l.conn == nil || l.conn.Conn().IsClosed() {
		l.mu.Unlock()
		return nil, ErrUnavailable
	}
	var held bool
	if e := l.conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory' AND classid=0 AND objid=762019001 AND granted)").Scan(&held); e != nil || !held {
		l.mu.Unlock()
		return nil, ErrUnavailable
	}
	tx, e := l.conn.Begin(ctx)
	if e != nil {
		l.mu.Unlock()
		return nil, e
	}
	return &leaseTx{Tx: tx, unlock: l.mu.Unlock}, nil
}
func (s *Store) beginLive(ctx context.Context) (pgx.Tx, error) {
	if s.liveLease != nil {
		return s.liveLease.begin(ctx)
	}
	return s.Pool.Begin(ctx)
}
