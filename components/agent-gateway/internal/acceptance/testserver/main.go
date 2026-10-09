// Local integration fixture. Uses the production authenticated Gateway HTTP server
// and schema; advertises test runtime support without starting cloud resources.
package main

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/db"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/httpapi"
	"net"
	"net/http"
	"os"
)

func main() {
	ctx := context.Background()
	p, e := pgxpool.New(ctx, os.Getenv("TEST_DATABASE_URL"))
	if e != nil {
		panic(e)
	}
	defer p.Close()
	if e = db.Migrate(ctx, p); e != nil {
		panic(e)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		panic(e)
	}
	fmt.Println("http://" + l.Addr().String())
	s := &execution.Store{Pool: p, RuntimeMode: "live", ConversationEnabled: true, ConversationRuntime: true}
	if e = http.Serve(l, httpapi.New(s, "fixture-service", "fixture-password")); e != nil {
		panic(e)
	}
}
