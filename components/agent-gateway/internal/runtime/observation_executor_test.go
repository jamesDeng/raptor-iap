package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGatewayObservationPrivateInputAndDeadline(t *testing.T) {
	in := proxyStart()
	in.Access.Credential = "raptor_at_" + strings.Repeat("a", 43)
	in.Access.InfraMcpURL = "https://infra.fixture/mcp"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	material := ObservationMaterial{Config: []byte(`{"private":{"infra":{"url":"https://evil.fixture/mcp","credential":"wrong"}}}`), Kubeconfig: []byte(`{}`)}
	raw, deadline, err := gatewayObservationInput(ctx, in, material, "cloud_read", json.RawMessage(`{"kind":"fleet"}`))
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Infra      struct{ URL, Credential string }
		DeadlineAt time.Time
	}
	if json.Unmarshal(raw, &input) != nil || input.Infra.URL != in.Access.InfraMcpURL || input.Infra.Credential != in.Access.Credential {
		t.Fatal("private access did not come from trusted LiveStart")
	}
	if time.Until(deadline) > time.Second || !deadline.Equal(input.DeadlineAt) {
		t.Fatal("shorter context deadline extended")
	}
	for _, change := range []string{"binding", "scope", "credential", "url", "expiry", "cancel"} {
		t.Run(change, func(t *testing.T) {
			changed := in
			scope := *in.Access.ReplacementScope
			changed.Access.ReplacementScope = &scope
			callCtx := ctx
			switch change {
			case "binding":
				changed.Access.Binding.RequestID = "other"
			case "scope":
				scope.GroupID = ""
			case "credential":
				changed.Access.Credential = ""
			case "url":
				changed.Access.InfraMcpURL = "https://infra.fixture/mcp?secret=x"
			case "expiry":
				changed.Access.ExpiresAt = time.Now().Add(-time.Second)
			case "cancel":
				c, stop := context.WithCancel(ctx)
				stop()
				callCtx = c
			}
			if _, _, err := gatewayObservationInput(callCtx, changed, material, "cloud_read", json.RawMessage(`{"kind":"fleet"}`)); err == nil {
				t.Fatal("unsafe private input accepted")
			}
		})
	}
}
