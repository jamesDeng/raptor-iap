package runtime

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
	"net"
	"testing"
	"time"
)

// Emulate a transaction pool without named prepared-statement support. Verify
// the actual PostgreSQL wire message, not a configuration-field assertion.
func TestObserverSelectOneUsesSimpleProtocolThroughTransactionPool(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	result := make(chan error, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			result <- e
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		b := pgproto3.NewBackend(conn, conn)
		if _, e = b.ReceiveStartupMessage(); e != nil {
			result <- e
			return
		}
		b.Send(&pgproto3.AuthenticationOk{})
		b.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "14.0"})
		b.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
		b.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
		b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		if e = b.Flush(); e != nil {
			result <- e
			return
		}
		m, e := b.Receive()
		if e != nil {
			result <- e
			return
		}
		q, ok := m.(*pgproto3.Query)
		if !ok || q.String != "SELECT 1" {
			b.Send(&pgproto3.ErrorResponse{Severity: "ERROR", Code: "26000", Message: "fixture pool rejects prepared statement"})
			b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
			b.Flush()
			result <- fmt.Errorf("fixed probe sent %T instead of SELECT1 Query", m)
			return
		}
		b.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{{Name: []byte("one"), DataTypeOID: 23, DataTypeSize: 4, TypeModifier: -1, Format: 0}}})
		b.Send(&pgproto3.DataRow{Values: [][]byte{[]byte("1")}})
		b.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
		b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		result <- b.Flush()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, e := pgx.ParseConfig("host=127.0.0.1 sslmode=disable user=fixture dbname=test")
	if e != nil {
		t.Fatal(e)
	}
	cfg.Port = uint16(listener.Addr().(*net.TCPAddr).Port)
	conn, e := pgx.ConnectConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close(context.Background())
	if e = observerSelectOne(ctx, conn); e != nil {
		t.Error("fixed probe failed", e)
	}
	if e = <-result; e != nil {
		t.Fatal(e)
	}
}
