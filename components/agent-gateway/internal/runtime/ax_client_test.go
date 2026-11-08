package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

func TestAXEndpointRequiresPrivateHTTPS(t *testing.T) {
	for _, v := range []string{"http://10.70.1.180:8443", "https://43.106.61.98:8443", "https://10.70.1.180:8443/?x=1", "https://user:pass@10.70.1.180:8443"} {
		if validateAXEndpoint(v) == nil {
			t.Fatalf("unsafe endpoint %s accepted", v)
		}
	}
	if validateAXEndpoint("https://10.70.1.180:8443") != nil {
		t.Fatal("private endpoint rejected")
	}
}

func TestAXBridgeWireKeepsRequestGrantInTheDefinitionBinding(t *testing.T) {
	binding := execution.AttemptBinding{
		RequestID: "request", AttemptID: "attempt", EnvCode: "rdev.ali",
		ObjectKind: "db-proxy", ObjectCode: "DP00001", Operation: "db-proxy.replace-nodes",
		ExecutionMode: "replacement", DefinitionSHA256: "grant-bound-definition",
		ApprovalProtocol: "request-grants-v1",
	}
	want := binding
	want.ApprovalProtocol = ""
	for _, action := range []string{"ensure", "remove"} {
		t.Run(action, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var received AXRequest
				if err := json.NewDecoder(r.Body).Decode(&received); err != nil || received.Action != action || received.Binding != want {
					t.Errorf("AX bridge received an incompatible request: %+v (%v)", received, err)
					http.Error(w, "InvalidRuntimeRequest", http.StatusBadRequest)
					return
				}
				_ = json.NewEncoder(w).Encode(AXResponse{Absent: action == "remove"})
			}))
			defer server.Close()

			client := AXClient{endpoint: server.URL, http: server.Client()}
			request := AXRequest{Action: action, Binding: binding}
			got, err := client.Call(context.Background(), request)
			if err != nil || got.Absent != (action == "remove") {
				t.Fatalf("AX bridge %s failed: %+v (%v)", action, got, err)
			}
			if request.Binding != binding {
				t.Fatal("Gateway request binding was modified")
			}
		})
	}
}
