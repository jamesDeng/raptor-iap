// sandbox-compat is dry by default. Execute only after separately approved
// cloud compatibility authorization; it performs no provider/model inference.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/db"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	adapter "github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/runtime"
	"os"
	"time"
)

func main() {
	execute := flag.Bool("execute", false, "Execute authorized compatibility using an exclusively leased Gateway journal")
	request := flag.String("request-id", "", "Explicit queued compatibility request UUID")
	flag.Parse()
	if !*execute {
		fmt.Println(`{"dryRun":true,"modelCalls":0,"cloudCalls":0}`)
		return
	}
	if run(*request) != nil {
		fmt.Println(`{"passed":false,"failure":"CompatibilityUnavailable","modelCalls":0}`)
		os.Exit(1)
	}
}
func run(request string) error {
	if request == "" {
		return execution.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()
	var config adapter.LiveConfig
	var credential adapter.ControllerCredential
	if adapter.ReadPrivateJSON(os.Getenv("GATEWAY_LIVE_CONFIG_FILE"), &config) != nil || adapter.ReadPrivateJSON(os.Getenv("GATEWAY_CONTROLLER_CREDENTIAL_FILE"), &credential) != nil {
		return execution.ErrInvalid
	}
	management, verifier, e := adapter.NewCloudClients(config, credential)
	if e != nil {
		return e
	}
	if management.Preflight(ctx, config) != nil {
		return execution.ErrUnavailable
	}
	checkpoint, e := verifier.VerifyCheckpoint(ctx, config.BootstrapCheckpoint)
	if e != nil {
		return e
	}
	pool, e := db.Open(ctx, os.Getenv("GATEWAY_DATABASE_URL"))
	if e != nil {
		return e
	}
	defer pool.Close()
	store := &execution.Store{Pool: pool, RuntimeMode: "live"}
	lease, e := store.AcquireLiveWorker(ctx)
	if e != nil {
		return e
	}
	defer lease.Close()
	owner := execution.NewID()
	// No old attempt is taken over and no unrelated queue entry is claimed.
	id, _, e := store.ActiveOwner(ctx)
	if e != nil || id != "" {
		return execution.ErrUnavailable
	}
	x, e := store.ClaimForRequest(ctx, owner, request)
	if e != nil || x == nil {
		return execution.ErrUnavailable
	}
	raptor := execution.HTTPRaptor{BaseURL: os.Getenv("RAPTOR_OPEN_API_URL"), Username: os.Getenv("SERVICE_USERNAME"), Password: os.Getenv("SERVICE_PASSWORD")}
	input, e := raptor.Context(ctx, request)
	if e != nil {
		store.RejectLiveClaim(ctx, x.AttemptID, owner, "InvalidResult")
		return execution.ErrInvalid
	}
	binding, _, hash, e := execution.ParseLiveQuestion(input, x.AttemptID)
	if e != nil {
		store.RejectLiveClaim(ctx, x.AttemptID, owner, "InvalidResult")
		return execution.ErrInvalid
	}
	if e = store.BindLive(ctx, x.AttemptID, owner, binding, hash); e != nil {
		return e
	}
	runtime := &adapter.NativeLive{Config: config, Management: management, Verifier: verifier, Store: store, Owner: owner, Lease: lease}
	report := runtime.Compatibility(ctx, binding, checkpoint, time.Now().Add(600*time.Second))
	finish, c := context.WithTimeout(context.Background(), 30*time.Second)
	defer c()
	// Compatibility is not an answered application question; never label it completed.
	e = store.FinalizeLive(finish, x.AttemptID, owner, execution.LiveOutcome{Status: "failed", FailureCode: "Interrupted", SandboxAbsent: report.SandboxAbsent, KeyAbsent: report.KeyAbsent, AccessRevoked: true})
	json.NewEncoder(os.Stdout).Encode(report)
	if e != nil {
		return e
	}
	if !report.Passed {
		return execution.ErrUnavailable
	}
	return nil
}
