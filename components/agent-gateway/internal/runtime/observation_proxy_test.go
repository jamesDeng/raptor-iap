package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func proxyStart() execution.LiveStart {
	b := execution.AttemptBinding{RequestID: "request", AttemptID: "attempt", DefinitionSHA256: "hash", EnvCode: "rdev.ali", ClusterID: "cluster", Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", ObjectCode: "proxy"}
	scope := execution.ReplacementScope{EnvCode: b.EnvCode, ProxyCode: b.ObjectCode, TargetDBCode: "db", GroupID: "group", ServerGroupID: "backend", Application: execution.ApplicationScope{EnvCode: b.EnvCode, AppCode: "app", ClusterID: b.ClusterID, Namespace: "raptor-test", Name: "client", UID: "uid"}}
	return execution.LiveStart{Binding: b, Access: execution.AgentAccess{Binding: b, ReplacementScope: &scope, ExpiresAt: time.Now().Add(180 * time.Second)}, Deadline: time.Now().Add(180 * time.Second)}
}
func TestObservationProxyBindsAndRevokes(t *testing.T) {
	calls := 0
	allowed := true
	p, e := NewObservationProxy("https://api.rdev.raptor-iap.top/v1/gateway-observations", func(ctx context.Context, in execution.LiveStart, m ObservationMaterial, tool string, args json.RawMessage) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"replicas":2}`), nil
	})
	if e != nil {
		t.Fatal(e)
	}
	in := proxyStart()
	m := ObservationMaterial{Config: []byte(`{"private":{"password":"do-not-deliver"}}`), Kubeconfig: []byte("private-kube")}
	guest, e := p.Register(in, m, func(context.Context) error {
		if !allowed {
			return errors.New("lost")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	var c struct {
		Token string `json:"token"`
	}
	json.Unmarshal(guest.Config, &c)
	if len(c.Token) < 32 || strings.Contains(string(guest.Config), "do-not-deliver") || len(guest.Kubeconfig) != 0 {
		t.Fatal("credential isolation")
	}
	invoke := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/gateway-observations", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+c.Token)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		return w
	}
	raw := `{"attemptId":"attempt","requestId":"request","definitionSha256":"hash","tool":"deployment_read","arguments":{}}`
	if w := invoke(raw); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, bad := range []string{strings.Replace(raw, "request\"", "other\"", 1), strings.Replace(raw, "deployment_read", "restart", 1), strings.Replace(raw, `"arguments":{}`, `"arguments":{},"url":"http://evil"`, 1), raw + `{}`} {
		if w := invoke(bad); w.Code == 200 {
			t.Fatal("bad request executed", bad)
		}
	}
	allowed = false
	if w := invoke(raw); w.Code != 401 {
		t.Fatal("lost owner accepted", w.Code)
	}
	if calls != 1 {
		t.Fatal("invalid backend calls", calls)
	}
	p.Remove(in.Binding.AttemptID)
	allowed = true
	if w := invoke(raw); w.Code != 401 {
		t.Fatal("retired token accepted")
	}
}
func TestObservationProxySanitizesResult(t *testing.T) {
	p, _ := NewObservationProxy("https://api.rdev.raptor-iap.top/v1/gateway-observations", func(context.Context, execution.LiveStart, ObservationMaterial, string, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"secret":"do-not-export"}`), nil
	})
	m, _ := p.Register(proxyStart(), ObservationMaterial{Secrets: []string{"do-not-export"}}, func(context.Context) error { return nil })
	var c map[string]any
	json.Unmarshal(m.Config, &c)
	r := httptest.NewRequest("POST", "/v1/gateway-observations", strings.NewReader(`{"attemptId":"attempt","requestId":"request","definitionSha256":"hash","tool":"deployment_read","arguments":{}}`))
	r.Header.Set("Authorization", "Bearer "+c["token"].(string))
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "do-not-export") {
		t.Fatal("unsafe result", w.Code)
	}
}
func TestObservationProxyRejectsExpiredAndConcurrentAttempt(t *testing.T) {
	p, _ := NewObservationProxy("https://api.rdev.raptor-iap.top/v1/gateway-observations", func(context.Context, execution.LiveStart, ObservationMaterial, string, json.RawMessage) (json.RawMessage, error) {
		return nil, nil
	})
	in := proxyStart()
	in.Deadline = time.Now().Add(-time.Second)
	if _, e := p.Register(in, ObservationMaterial{}, func(context.Context) error { return nil }); e == nil {
		t.Fatal("expired")
	}
	in = proxyStart()
	if _, e := p.Register(in, ObservationMaterial{}, func(context.Context) error { return nil }); e != nil {
		t.Fatal(e)
	}
	in.Binding.AttemptID = "second"
	in.Access.Binding = in.Binding
	if _, e := p.Register(in, ObservationMaterial{}, func(context.Context) error { return nil }); e == nil {
		t.Fatal("concurrent")
	}
}
func TestObservationProxyRejectsExpiredSource(t *testing.T) {
	p, _ := NewObservationProxy("https://api.rdev.raptor-iap.top/v1/gateway-observations", func(context.Context, execution.LiveStart, ObservationMaterial, string, json.RawMessage) (json.RawMessage, error) {
		return nil, nil
	})
	raw, _ := json.Marshal(map[string]any{"expiresAt": time.Now().Add(-time.Second)})
	if _, e := p.Register(proxyStart(), ObservationMaterial{Config: raw}, func(context.Context) error { return nil }); e == nil {
		t.Fatal("expired underlying credentials registered")
	}
}
