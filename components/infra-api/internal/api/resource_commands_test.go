package api

import (
	"context"
	"encoding/json"
	"net/http"
	"raptor-iap/infra-api/internal/commands"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
	"raptor-iap/infra-api/internal/resources"
	"reflect"
	"strings"
	"testing"
)

type resourceCommander struct {
	commandFixture
	env    domain.Environment
	target any
}

func (f *resourceCommander) Restart(_ context.Context, e domain.Environment, c domain.RestartCommand) (domain.RestartReceipt, error) {
	f.calls++
	f.env = e
	f.target = c
	return domain.RestartReceipt{Accepted: true}, nil
}
func (f *resourceCommander) Scale(_ context.Context, e domain.Environment, c domain.ScaleCommand) (domain.ScaleReceipt, error) {
	f.calls++
	f.env = e
	f.target = c
	return domain.ScaleReceipt{Outcome: "accepted"}, nil
}
func (f *resourceCommander) SetProtection(_ context.Context, e domain.Environment, c domain.ProtectionCommand) (domain.NodeReceipt, error) {
	f.calls++
	f.env = e
	f.target = c
	return domain.NodeReceipt{Outcome: "accepted"}, nil
}
func (f *resourceCommander) Deregister(_ context.Context, e domain.Environment, c domain.DeregisterCommand) (domain.NodeReceipt, error) {
	f.calls++
	f.env = e
	f.target = c
	return domain.NodeReceipt{Outcome: "accepted"}, nil
}
func replacementResourceIdentity() (AgentIdentity, resources.Proof) {
	i := resourceIdentity()
	i.Binding.ObjectKind = "db-proxy"
	i.Binding.ObjectCode = "p"
	i.Binding.Operation = "db-proxy.replace-nodes"
	i.Binding.ExecutionMode = "replacement"
	s := AgentReplacementScope{EnvCode: "dev", ProxyCode: "p", GroupID: "g", ServerGroupID: "sg", TargetDBCode: "db", Application: AgentApplication{EnvCode: "dev", AppCode: "app", ClusterID: "c", Namespace: "n", Name: "app", UID: "uid"}, AccountID: "123", Region: "ap-southeast-1", ListenerID: "l", Port: 5432, DatabaseResourceID: "rds"}
	p := resources.Proof{Identity: resources.ResolutionIdentity{EnvCode: "dev", AccountID: "123", Region: "ap-southeast-1", ClusterID: "c", ProxyCode: "p", GroupID: "g", ServerGroupID: "sg", ListenerID: "l", Port: 5432, TargetDBCode: "db", DatabaseResourceID: "rds", Application: resources.ApplicationIdentity{EnvCode: "dev", AppCode: "app", ClusterID: "c", Namespace: "n", Name: "app", UID: "uid"}}}
	p.ResolutionDigest, _ = resources.Digest(p.Identity)
	s.ResolutionDigest = p.ResolutionDigest
	i.ReplacementScope = &s
	return i, p
}
func resourceCommandHandler(t *testing.T, f *resourceFixture, cmd Commander, i *AgentIdentity) http.Handler {
	t.Helper()
	var check AgentCheck
	if i != nil {
		check = func(_ context.Context, _ string, tool string, s map[string]string) (AgentIdentity, error) {
			if tool == "" {
				t.Error("unexpected empty precheck")
			}
			return *i, nil
		}
	}
	h, e := NewResourceServer(&observedReader{}, f, "user", "secret", "X-Infra-Authorization", cmd, allowCommands{}, check)
	if e != nil {
		t.Fatal(e)
	}
	return h
}
func TestResourceCommandsUseExactResolvedTargets(t *testing.T) {
	for _, tc := range []struct {
		path, body string
		target     any
	}{
		{"/v1/db-proxy-scale", `{"requestId":"r","actionId":"action","envCode":"dev","proxyCode":"p","desiredCapacity":0}`, domain.ScaleCommand{ProxyTarget: domain.ProxyTarget{RequestID: "r", EnvCode: "dev", ProxyCode: "p", GroupID: "g"}, ActionID: "action", DesiredCapacity: 0}},
		{"/v1/db-proxy-node-protection", `{"requestId":"r","envCode":"dev","proxyCode":"p","nodeIds":["i"],"protected":false}`, domain.ProtectionCommand{ProxyTarget: domain.ProxyTarget{RequestID: "r", EnvCode: "dev", ProxyCode: "p", GroupID: "g"}, InstanceIDs: []string{"i"}, Protected: false}},
		{"/v1/db-proxy-node-deregistration", `{"requestId":"r","envCode":"dev","proxyCode":"p","nodeIds":["i"]}`, domain.DeregisterCommand{ProxyTarget: domain.ProxyTarget{RequestID: "r", EnvCode: "dev", ProxyCode: "p", GroupID: "g"}, ServerGroupID: "sg", InstanceIDs: []string{"i"}}},
		{"/v1/deployment-restart", `{"requestId":"r","envCode":"dev","appCode":"app","workloadKey":"wk"}`, domain.RestartCommand{RequestID: "r", EnvCode: "dev", AppCode: "app", ClusterID: "c", Namespace: "n", Name: "app", UID: "uid"}},
	} {
		for _, agent := range []bool{false, true} {
			f := &resourceFixture{rows: resourceRows()}
			cmd := &resourceCommander{}
			var identity *AgentIdentity
			auth := valid()
			if agent {
				i, p := replacementResourceIdentity()
				identity = &i
				f.proof = p
				auth = agentCredential()
			}
			w := resourceHTTP(resourceCommandHandler(t, f, cmd, identity), "POST", tc.path, tc.body, auth)
			if w.Code != 200 || cmd.calls != 1 || !reflect.DeepEqual(cmd.target, tc.target) || cmd.env.AccountID != "123" {
				t.Fatalf("%s agent=%v %d %s %#v", tc.path, agent, w.Code, w.Body.String(), cmd.target)
			}
		}
	}
}
func TestResourceCommandsRequireCompleteMatchingPrivateProof(t *testing.T) {
	for _, change := range []string{"missing", "digest", "listener", "database", "uid", "port", "resolved-group", "resolved-app"} {
		t.Run(change, func(t *testing.T) {
			i, p := replacementResourceIdentity()
			f := &resourceFixture{rows: resourceRows(), proof: p}
			switch change {
			case "missing":
				i.ReplacementScope.ResolutionDigest = ""
			case "digest":
				f.proof.ResolutionDigest = strings.Repeat("0", 64)
			case "listener":
				f.proof.Identity.ListenerID = "other"
			case "database":
				f.proof.Identity.DatabaseResourceID = "other"
			case "uid":
				f.proof.Identity.Application.UID = "other"
			case "port":
				f.proof.Identity.Port = 6432
			case "resolved-group":
				f.rows.Proxy.GroupID = "other"
			case "resolved-app":
				f.rows.ApplicationWorkloads[0].Target.UID = "other"
			}
			cmd := &resourceCommander{}
			path, body := "/v1/db-proxy-scale", `{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"p","desiredCapacity":4}`
			if change == "resolved-app" {
				path = "/v1/deployment-restart"
				body = `{"requestId":"r","envCode":"dev","appCode":"app","workloadKey":"wk"}`
			}
			w := resourceHTTP(resourceCommandHandler(t, f, cmd, &i), "POST", path, body, agentCredential())
			if w.Code != 409 || cmd.calls != 0 {
				t.Fatal(w.Code, w.Body.String(), cmd.calls)
			}
		})
	}
}
func TestResourceQualificationAndForeignCommandsStopBeforeResolver(t *testing.T) {
	for _, qualification := range []bool{true, false} {
		i, p := replacementResourceIdentity()
		if qualification {
			i.Binding.ExecutionMode = "qualification"
		}
		f := &resourceFixture{rows: resourceRows(), proof: p}
		cmd := &resourceCommander{}
		proxy := "foreign"
		if qualification {
			proxy = "p"
		}
		body := `{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"` + proxy + `","desiredCapacity":4}`
		w := resourceHTTP(resourceCommandHandler(t, f, cmd, &i), "POST", "/v1/db-proxy-scale", body, agentCredential())
		if w.Code != 403 || cmd.calls != 0 || f.calls != 0 || f.proofs != 0 {
			t.Fatal(w.Code, cmd.calls, f.calls, f.proofs)
		}
	}
}
func TestResourceCommandInputStrictnessAndSchemas(t *testing.T) {
	body := `{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"p","desiredCapacity":4}`
	for _, raw := range []string{strings.Replace(body, `"desiredCapacity":4`, `"desiredCapacity":null`, 1), strings.Replace(body, `"desiredCapacity":4`, `"desiredCapacity":4,"groupId":"g"`, 1), strings.Replace(body, `"proxyCode":"p"`, `"proxyCode":"p","proxyCode":"p"`, 1), strings.Replace(body, `"proxyCode":"p"`, `"proxyCode":2`, 1), strings.Replace(body, `"proxyCode":"p"`, `"proxyCode":"https://evil?groupId=g"`, 1)} {
		f := &resourceFixture{}
		cmd := &resourceCommander{}
		w := resourceHTTP(resourceCommandHandler(t, f, cmd, nil), "POST", "/v1/db-proxy-scale", raw, valid())
		if w.Code != 400 || cmd.calls != 0 || f.calls != 0 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, spec := range resourceCommandSpecs {
		raw, _ := json.Marshal(commandSchema(spec.sample))
		for _, forbidden := range []string{"groupId", "serverGroupId", "clusterId", "namespace", "uid", "instanceIds"} {
			if strings.Contains(string(raw), `"`+forbidden+`"`) {
				t.Fatal(spec.name, string(raw))
			}
		}
	}
}

type resourceRuntimeBackend struct {
	writes        int
	snapshotProxy string
}

func (b *resourceRuntimeBackend) Snapshot(_ context.Context, _ domain.Environment, code string) ([]policy.ProxySnapshot, error) {
	b.snapshotProxy = code
	return []policy.ProxySnapshot{{EnvCode: "dev", ProxyCode: "p", GroupID: "g", ServerGroupID: "sg", Desired: 2, Min: 1, Max: 4, Nodes: []policy.Node{{ID: "member", Registered: true, Healthy: true}, {ID: "retained", Registered: true, Healthy: true}}}}, nil
}
func (b *resourceRuntimeBackend) Scale(context.Context, domain.Environment, string, int) (string, error) {
	b.writes++
	return "", nil
}
func (b *resourceRuntimeBackend) Protect(context.Context, domain.Environment, string, []string, bool) error {
	b.writes++
	return nil
}
func (b *resourceRuntimeBackend) Deregister(context.Context, domain.Environment, string, []string) error {
	b.writes++
	return nil
}
func TestResourceNodeIDsRemainWithinCurrentRuntimeMembership(t *testing.T) {
	for _, path := range []string{"/v1/db-proxy-node-protection", "/v1/db-proxy-node-deregistration"} {
		f := &resourceFixture{rows: resourceRows()}
		backend := &resourceRuntimeBackend{}
		runtime := commands.Runtime{Backend: backend, EvidenceMode: "synthetic"}
		body := `{"requestId":"r","envCode":"dev","proxyCode":"p","nodeIds":["foreign"]`
		if path == "/v1/db-proxy-node-protection" {
			body += `,"protected":false`
		}
		body += `}`
		w := resourceHTTP(resourceCommandHandler(t, f, runtime, nil), "POST", path, body, valid())
		if w.Code == 200 || backend.writes != 0 || backend.snapshotProxy != "p" {
			t.Fatal(w.Code, w.Body.String(), backend.writes, backend.snapshotProxy)
		}
	}
}

func TestResourceCommandsRejectCaseFoldedAndNullSelectors(t *testing.T) {
	base := `{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"p","desiredCapacity":4}`
	for _, raw := range []string{strings.Replace(base, `"envCode":"dev"`, `"EnvCode":"dev"`, 1), strings.Replace(base, `"envCode":"dev"`, `"envCode":"dev","EnvCode":"other"`, 1), strings.Replace(base, `"proxyCode":"p"`, `"proxyCode":"p","ProxyCode":"other"`, 1), strings.Replace(base, `"desiredCapacity":4`, `"DesiredCapacity":4`, 1)} {
		f := &resourceFixture{rows: resourceRows()}
		cmd := &resourceCommander{}
		w := resourceHTTP(resourceCommandHandler(t, f, cmd, nil), "POST", "/v1/db-proxy-scale", raw, valid())
		if w.Code != 400 || cmd.calls != 0 || f.calls != 0 {
			t.Fatal(w.Code, w.Body.String(), cmd.calls, f.calls)
		}
	}
}
