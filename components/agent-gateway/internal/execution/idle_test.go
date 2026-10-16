package execution

import (
	"context"
	"testing"
)

func TestIdleCheck(t *testing.T) {
	for _, state := range []string{"idle", "queued", "running", "paused", "waiting_resource", "held", "unresolved", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			if state == "unavailable" {
				s.Pool.Close()
			} else if state == "held" || state == "unresolved" {
				if _, e := s.Pool.Exec(ctx, "UPDATE gateway.runtime_slot SET owner=CASE WHEN $1 THEN 'worker' ELSE NULL END,unresolved=$2", state == "held", state == "unresolved"); e != nil {
					t.Fatal(e)
				}
			} else if state != "idle" {
				id := NewID()
				status := state
				if state == "paused" || state == "running" {
					status = "completed" // isolate unfinished-attempt proof from execution status
				}
				if _, e := s.Pool.Exec(ctx, "INSERT INTO gateway.executions(request_id,status) VALUES($1,$2)", id, status); e != nil {
					t.Fatal(e)
				}
				if state == "running" || state == "paused" {
					if _, e := s.Pool.Exec(ctx, "INSERT INTO gateway.attempts(id,request_id,state) VALUES($1,$2,$3)", NewID(), id, state); e != nil {
						t.Fatal(e)
					}
				}
			}
			if (s.CheckIdle(ctx) == nil) != (state == "idle") {
				t.Fatalf("wrong idle result for %s", state)
			}
		})
	}
}
