package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPDecisionRechecksControlAndExactApproval(t *testing.T) {
	binding := map[string]any{"requestId": "request", "actionId": "action", "interface": "ess.scale-in", "envCode": "dev", "target": map[string]string{"groupId": "group", "proxyCode": "proxy"}, "parameters": map[string]int{"desiredCapacity": 3}}
	raw, _ := json.Marshal(binding)
	sum := sha256.Sum256(raw)
	wait := LiveWait{Kind: "approval", ApprovalID: "approval", ActionID: "action", BindingDigest: hex.EncodeToString(sum[:])}
	control := ""
	state := "approved"
	seen := 0
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "service" || p != "fixture" {
			t.Error("missing private service auth")
			w.WriteHeader(401)
			return
		}
		seen++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/requests/request/control":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"requestId": "request", "status": "waiting_approval", "controlState": control}})
		case "/v1/requests/request/approvals/approval":
			json.NewEncoder(w).Encode(map[string]any{"data": LiveApprovalDecision{RequestID: "request", ApprovalID: "approval", ActionID: "action", Binding: raw, State: state}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer web.Close()
	client := HTTPRaptor{BaseURL: web.URL, Username: "service", Password: "fixture"}
	out, e := client.ResolveLiveDecision(context.Background(), "request", wait)
	if e != nil || out.Next != "resume" || seen != 2 {
		t.Fatal("fresh lookup failed", e)
	}
	control = "cancel_requested"
	out, e = client.ResolveLiveDecision(context.Background(), "request", wait)
	if e != nil || out.Next != "cancel" {
		t.Fatal("cancellation not respected", e)
	}
	control = ""
	state = "submitted"
	if _, e = client.ResolveLiveDecision(context.Background(), "request", wait); e == nil {
		t.Fatal("submitted action replayed")
	}
}

func TestHTTPGrantDecisionRechecksBundleAndDenial(t *testing.T) {
	const grantID = "33333333-3333-4333-8333-333333333333"
	wait := LiveWait{Kind: "grant", ApprovalID: grantID, ActionID: grantID,
		BindingDigest: strings.Repeat("a", 64), StartedAt: time.Now().Add(-time.Second)}
	state := "pending"
	digest := wait.BindingDigest
	requestID := "request"
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/requests/request/control":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"requestId": "request", "status": "running", "controlState": ""}})
		case "/v1/requests/request/grants/" + grantID:
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"requestId": requestID, "id": grantID, "bundleDigest": digest, "state": state, "version": 1,
			}})
		default:
			t.Error("grant wait read the legacy approval endpoint")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer web.Close()
	client := HTTPRaptor{BaseURL: web.URL, Username: "service", Password: "fixture"}
	for _, test := range []struct{ state, next string }{{"pending", "waiting"}, {"approved", "resume"}, {"denied", "block"}} {
		state = test.state
		out, err := client.ResolveLiveDecision(context.Background(), "request", wait)
		if err != nil || out.Next != test.next {
			t.Fatalf("state %s: %+v %v", test.state, out, err)
		}
	}
	state = "approved"
	digest = strings.Repeat("b", 64)
	if _, err := client.ResolveLiveDecision(context.Background(), "request", wait); err == nil {
		t.Fatal("wrong bundle digest resumed")
	}
	digest = wait.BindingDigest
	requestID = "foreign"
	if _, err := client.ResolveLiveDecision(context.Background(), "request", wait); err == nil {
		t.Fatal("foreign request resumed")
	}
}
