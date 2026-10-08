package traffic

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPinnedPgCatCountsIdleActiveAndMultiplePools(t *testing.T) {
	file := os.Getenv("TEST_LOCAL_PGCAT_DSN_FILE")
	if file == "" {
		t.Skip("pinned local PgCat fixture required")
	}
	raw, e := os.ReadFile(file)
	if e != nil {
		t.Fatal("fixture unreadable")
	}
	cfg, e := pgx.ParseConfig(strings.TrimSpace(string(raw)))
	if e != nil || cfg.Host != "127.0.0.1" {
		t.Fatal("local-only fixture required")
	}
	endpoint := "http://127.0.0.1:29930/metrics"
	client := &http.Client{Timeout: time.Second}
	metrics := func() map[string]int {
		r, e := client.Get(endpoint)
		if e != nil {
			t.Fatal("metrics unavailable")
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatal(r.StatusCode)
		}
		body, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if e != nil {
			t.Fatal(e)
		}
		m := map[string]int{}
		for _, line := range strings.Split(string(body), "\n") {
			for _, name := range []string{"pgcat_pools_cl_idle", "pgcat_pools_cl_active", "pgcat_pools_cl_waiting"} {
				if strings.HasPrefix(line, name+"{") {
					fields := strings.Fields(line)
					v, e := strconv.Atoi(fields[len(fields)-1])
					if e != nil {
						t.Fatal(e)
					}
					m[name] += v
				}
			}
		}
		return m
	}
	expect := func(total int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			m := metrics()
			if len(m) == 3 && m["pgcat_pools_cl_idle"]+m["pgcat_pools_cl_active"]+m["pgcat_pools_cl_waiting"] == total {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("client count did not converge", total)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	expect(0)
	a, e := pgx.ConnectConfig(ctx, cfg)
	if e != nil {
		t.Fatal("connect failed")
	}
	defer a.Close(context.Background())
	expect(1)
	if metrics()["pgcat_pools_cl_idle"] != 1 {
		t.Fatal("idle client excluded")
	}
	done := make(chan error, 1)
	go func() { _, e := a.Exec(ctx, "SELECT pg_sleep(1)"); done <- e }()
	deadline := time.Now().Add(time.Second)
	active := false
	for time.Now().Before(deadline) {
		if metrics()["pgcat_pools_cl_active"] == 1 {
			active = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !active {
		t.Fatal("active client absent")
	}
	if e = <-done; e != nil {
		t.Fatal("query failed")
	}
	cfg2 := cfg.Copy()
	cfg2.Database = "test2"
	b, e := pgx.ConnectConfig(ctx, cfg2)
	if e != nil {
		t.Fatal("second pool failed")
	}
	defer b.Close(context.Background())
	expect(2)
	if _, e = b.Exec(ctx, "SELECT 1"); e != nil {
		t.Fatal("second pool SQL failed")
	}
	_ = b.Close(ctx)
	_ = a.Close(ctx)
	expect(0)
	fmt.Println("local pinned PgCat: zero/idle/active/multiple-pool counts passed")
}
