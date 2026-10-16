package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func fixture() map[string]any {
	return map[string]any{"apiVersion": "v1", "kind": "Secret", "type": "Opaque", "metadata": map[string]any{"namespace": namespace, "name": secretName, "resourceVersion": "10", "uid": "secret-uid"}, "data": map[string]any{"config.json": encode(map[string]any{"accountId": account, "region": region, "replacementEnabled": false, "replacementObserver": map[string]any{"scope": scopeFixture(), "clusterServer": "https://cluster.fixture", "dbInstanceId": "db-fixture", "loadBalancerId": "nlb-fixture", "listenerId": "listener-fixture", "prometheusUrl": "http://metrics.fixture", "clientTargets": []any{"http://client.fixture"}, "sqlBucket": "fixture-bucket", "sqlKey": "fixture/sql"}})}}
}
func scopeFixture() map[string]any {
	return map[string]any{"envCode": "rdev.ali", "proxyCode": "DP00001", "targetDbCode": "D00002", "groupId": group, "serverGroupId": serverGroup, "application": map[string]any{"envCode": "rdev.ali", "appCode": "A00003", "clusterId": cluster, "namespace": "raptor-test", "name": "rdev-test-client-client", "uid": applicationUID}}
}
func encode(v any) string { b, _ := json.Marshal(v); return base64.StdEncoding.EncodeToString(b) }
func raw(v any) []byte    { b, _ := json.Marshal(v); return b }
func config(s map[string]any) map[string]any {
	b, _ := base64.StdEncoding.DecodeString(s["data"].(map[string]any)["config.json"].(string))
	var c map[string]any
	json.Unmarshal(b, &c)
	return c
}
func changeConfig(s map[string]any, f func(map[string]any)) {
	c := config(s)
	f(c)
	s["data"].(map[string]any)["config.json"] = encode(c)
}

type fakeRunner struct {
	secretReads int
	staleSecond bool
	secret      []byte
	calls       []string
	input       []byte
	idleCalls   int
	failIdle    int
	conflict    bool
}

func (f *fakeRunner) Run(_ context.Context, args []string, in []byte) ([]byte, error) {
	a := strings.Join(args, " ")
	f.calls = append(f.calls, a)
	if strings.Contains(a, "--idle-check") {
		f.idleCalls++
		if f.idleCalls == f.failIdle {
			return []byte("synthetic-sensitive"), errors.New("synthetic-sensitive")
		}
		return []byte("idle=true\n"), nil
	}
	if strings.Contains(a, "get secret") {
		f.secretReads++
		if f.staleSecond && f.secretReads > 1 {
			var s map[string]any
			json.Unmarshal(f.secret, &s)
			obj(s["metadata"])["resourceVersion"] = "99"
			return raw(s), nil
		}
		return f.secret, nil
	}
	if strings.Contains(a, "replace") {
		f.input = in
		if f.conflict {
			return []byte("synthetic-sensitive"), errors.New("synthetic-sensitive")
		}
		var s map[string]any
		json.Unmarshal(in, &s)
		s["metadata"].(map[string]any)["resourceVersion"] = "11"
		return raw(s), nil
	}
	return nil, errors.New("unexpected command")
}
func TestSetGuard(t *testing.T) {
	for _, name := range []string{"enable", "restore", "namespace", "name", "key", "account", "region", "group", "server", "application", "missing-boolean", "string-boolean", "unknown", "nested-unknown", "missing-rv", "wrong-expected", "duplicate-key", "malformed", "conflict", "busy", "became-busy"} {
		t.Run(name, func(t *testing.T) {
			s := fixture()
			expected, next := false, true
			switch name {
			case "restore":
				expected, next = true, false
				changeConfig(s, func(c map[string]any) { c["replacementEnabled"] = true })
			case "namespace", "name":
				s["metadata"].(map[string]any)[name] = "wrong"
			case "key":
				s["data"].(map[string]any)["extra"] = "unexpected"
			case "account":
				changeConfig(s, func(c map[string]any) { c["accountId"] = "wrong" })
			case "region":
				changeConfig(s, func(c map[string]any) { c["region"] = "wrong" })
			case "missing-boolean":
				changeConfig(s, func(c map[string]any) { delete(c, "replacementEnabled") })
			case "string-boolean":
				changeConfig(s, func(c map[string]any) { c["replacementEnabled"] = "false" })
			case "unknown":
				changeConfig(s, func(c map[string]any) { c["unexpected"] = "synthetic-sensitive" })
			case "group", "server", "application", "nested-unknown":
				changeConfig(s, func(c map[string]any) {
					scope := c["replacementObserver"].(map[string]any)["scope"].(map[string]any)
					key := map[string]string{"group": "groupId", "server": "serverGroupId", "application": "application", "nested-unknown": "unexpected"}[name]
					scope[key] = "wrong"
				})
			case "wrong-expected":
				expected = true
				next = false
			case "duplicate-key":
				s["data"].(map[string]any)["config.json"] = base64.StdEncoding.EncodeToString([]byte(`{"replacementEnabled":false,"replacementEnabled":true}`))
			case "missing-rv":
				delete(s["metadata"].(map[string]any), "resourceVersion")
			}
			f := &fakeRunner{secret: raw(s), conflict: name == "conflict"}
			if name == "malformed" {
				f.secret = []byte("{")
			}
			if name == "busy" {
				f.failIdle = 1
			}
			if name == "became-busy" {
				f.failIdle = 2
			}
			_, err := setFlag(context.Background(), f, expected, next)
			success := name == "enable" || name == "restore"
			if (err == nil) != success {
				t.Fatalf("unexpected success=%v", err == nil)
			}
			if !success && name != "conflict" && f.input != nil {
				t.Fatal("rejected case replaced Secret")
			}
			if err != nil && strings.Contains(err.Error(), "synthetic-sensitive") {
				t.Fatal("sensitive error leaked")
			}
			if success {
				var got map[string]any
				json.Unmarshal(f.input, &got)
				changeConfig(s, func(c map[string]any) { c["replacementEnabled"] = next })
				if !reflect.DeepEqual(got, s) {
					t.Fatal("changed unrelated field")
				}
				if f.idleCalls != 2 {
					t.Fatal("missing fresh idle")
				}
			}
			if name == "conflict" && strings.Count(strings.Join(f.calls, "\n"), "replace") != 1 {
				t.Fatal("conflict retried")
			}
		})
	}
}
func TestCLIRejectsSnapshotAndSanitizes(t *testing.T) {
	for _, args := range [][]string{{"--config", "private.json"}, {"--namespace", "wrong", "--secret", secretName, "--expected", "false", "--set", "true"}, {"--expected", "false", "--set", "false"}} {
		var out bytes.Buffer
		f := &fakeRunner{}
		if run(context.Background(), args, f, &out) == nil {
			t.Fatal("accepted unsafe arguments")
		}
		if len(f.calls) != 0 || strings.Contains(out.String(), "private") {
			t.Fatal("unsafe side effect or output")
		}
	}
}

type rolloutRunner struct {
	podReads   int
	replacePod bool
	fakeRunner
	deployment map[string]any
	pods       map[string]any
	rs         map[string]any
	effective  []byte
	patched    bool
}

func (f *rolloutRunner) Run(ctx context.Context, args []string, in []byte) ([]byte, error) {
	a := strings.Join(args, " ")
	f.calls = append(f.calls, a)
	switch {
	case strings.Contains(a, "get deployment"):
		return raw(f.deployment), nil
	case strings.Contains(a, "get pods"):
		f.podReads++
		if f.replacePod && f.podReads > 1 {
			obj(obj(f.pods["items"].([]any)[0])["metadata"])["uid"] = "replaced-pod"
		}
		return raw(f.pods), nil
	case strings.Contains(a, "get replicaset"):
		return raw(f.rs), nil
	case strings.Contains(a, "cat /run/raptor/live/config.json"):
		return f.effective, nil
	case strings.Contains(a, "patch deployment"):
		f.patched = true
		return nil, nil
	default:
		return f.fakeRunner.Run(ctx, args, in)
	}
}
func rolloutFixture() *rolloutRunner {
	s := fixture()
	c := config(s)
	f := &rolloutRunner{fakeRunner: fakeRunner{secret: raw(s)}, effective: raw(c), deployment: map[string]any{"metadata": map[string]any{"name": deployment, "namespace": namespace, "uid": "deployment-uid", "resourceVersion": "20", "generation": 2}, "spec": map[string]any{"replicas": 1, "selector": map[string]any{"matchLabels": map[string]any{"app": "agent-gateway"}}}, "status": map[string]any{"observedGeneration": 2, "replicas": 1, "updatedReplicas": 1, "readyReplicas": 1, "availableReplicas": 1}}, rs: map[string]any{"metadata": map[string]any{"name": "gateway-rs", "uid": "rs-uid", "namespace": namespace, "ownerReferences": []any{map[string]any{"kind": "Deployment", "uid": "deployment-uid", "controller": true}}}}, pods: map[string]any{"items": []any{map[string]any{"metadata": map[string]any{"name": "gateway-pod", "namespace": namespace, "uid": "new-pod-uid", "ownerReferences": []any{map[string]any{"kind": "ReplicaSet", "name": "gateway-rs", "uid": "rs-uid", "controller": true}}}, "status": map[string]any{"phase": "Running", "startTime": "2026-10-16T01:01:00Z", "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}}}}}
	attachRuntime(f)
	return f
}
func TestVerifyAndRollout(t *testing.T) {
	for _, name := range []string{"ready", "old-pod", "unready", "stale-secret", "old-generation", "foreign-owner", "wrong-effective", "pod-replaced", "rollout", "rollout-busy", "rollout-stale", "rollout-concurrent-secret"} {
		t.Run(name, func(t *testing.T) {
			f := rolloutFixture()
			pod := obj(f.pods["items"].([]any)[0])
			rv := "10"
			mode := "verify"
			switch name {
			case "old-pod":
				obj(pod["status"])["startTime"] = "2026-10-16T00:00:00Z"
			case "unready":
				obj(pod["status"])["conditions"] = []any{}
			case "stale-secret", "rollout-stale":
				rv = "9"
			case "old-generation":
				obj(f.deployment["status"])["observedGeneration"] = 1
			case "foreign-owner":
				obj(obj(f.rs["metadata"])["ownerReferences"].([]any)[0])["uid"] = "foreign"
			case "pod-replaced":
				f.replacePod = true
			case "wrong-effective":
				f.effective = []byte("{}")
			case "rollout-concurrent-secret":
				f.staleSecond = true
			case "rollout-busy":
				f.failIdle = 1
			}
			if strings.HasPrefix(name, "rollout") {
				mode = "rollout"
			}
			var out bytes.Buffer
			e := run(context.Background(), []string{mode, "--expected", "false", "--resource-version", rv, "--changed-after", "2026-10-16T01:00:00Z"}, f, &out)
			success := name == "ready" || name == "rollout"
			if (e == nil) != success {
				t.Fatalf("unexpected success=%v", e == nil)
			}
			if f.patched != (name == "rollout") {
				t.Fatal("unsafe rollout")
			}
			if strings.Contains(out.String(), account) || strings.Contains(out.String(), "replacementObserver") {
				t.Fatal("config leaked")
			}
			if name == "ready" && !strings.Contains(out.String(), "new-pod-uid") {
				t.Fatal("missing Pod proof")
			}
		})
	}
}
func TestSetCLIOnlyReceipt(t *testing.T) {
	f := &fakeRunner{secret: raw(fixture())}
	var out bytes.Buffer
	if run(context.Background(), []string{"--expected", "false", "--set", "true"}, f, &out) != nil {
		t.Fatal("set failed")
	}
	var receipt map[string]any
	if json.Unmarshal(out.Bytes(), &receipt) != nil || len(receipt) != 3 || receipt["resourceVersion"] != "11" || receipt["replacementEnabled"] != true {
		t.Fatal("bad sanitized receipt")
	}
}
func TestUnknownCompositeField(t *testing.T) {
	s := fixture()
	changeConfig(s, func(c map[string]any) { c["qualificationEnabled observationUrl"] = "unexpected" })
	f := &fakeRunner{secret: raw(s)}
	if _, e := setFlag(context.Background(), f, false, true); e == nil || f.input != nil {
		t.Fatal("unknown composite key accepted")
	}
}

func livePodSpec() map[string]any {
	image := "ghcr.io/jamesdeng/raptor-iap-gateway@sha256:" + strings.Repeat("a", 64)
	return map[string]any{"containers": []any{map[string]any{"name": deployment, "image": image, "command": []any{"/usr/local/bin/gateway"}, "env": []any{map[string]any{"name": "GATEWAY_RUNTIME_MODE", "value": "live"}, map[string]any{"name": "GATEWAY_LIVE_CONFIG_FILE", "value": "/run/raptor/live/config.json"}}, "volumeMounts": []any{map[string]any{"name": "live-private", "mountPath": "/run/raptor/live", "readOnly": true}}}}, "initContainers": []any{map[string]any{"name": "live-private-files", "image": image, "command": []any{"/bin/sh", "-ec"}, "args": []any{"umask 077; install -m 0600 /live-config-source/config.json /live-private/config.json"}, "volumeMounts": []any{map[string]any{"name": "live-config-source", "mountPath": "/live-config-source", "readOnly": true}, map[string]any{"name": "live-private", "mountPath": "/live-private"}}}}, "volumes": []any{map[string]any{"name": "live-private", "emptyDir": map[string]any{"medium": "Memory", "sizeLimit": "1Mi"}}, map[string]any{"name": "live-config-source", "secret": map[string]any{"secretName": secretName}}}}
}
func attachRuntime(f *rolloutRunner) {
	spec := livePodSpec()
	obj(f.deployment["spec"])["template"] = map[string]any{"spec": spec}
	pod := obj(f.pods["items"].([]any)[0])
	pod["spec"] = livePodSpec()
	status := obj(pod["status"])
	image := obj(spec["containers"].([]any)[0])["image"]
	status["containerStatuses"] = []any{map[string]any{"name": deployment, "imageID": "docker-pullable://" + image.(string), "ready": true, "state": map[string]any{"running": map[string]any{"startedAt": "2026-10-16T01:01:00Z"}}}}
	status["initContainerStatuses"] = []any{map[string]any{"name": "live-private-files", "imageID": "docker-pullable://" + image.(string), "state": map[string]any{"terminated": map[string]any{"exitCode": 0}}}}
}
func TestVerifyLoadedConfigBinding(t *testing.T) {
	for _, name := range []string{"alternate-path", "nonlive", "wrong-source", "writable", "wrong-copy", "wrong-init-image", "wrong-image", "wrong-command", "shadow-mount", "extra-writer", "disk-volume", "wrong-runtime-image", "unfinished-init", "deployment-path", "init-env", "init-source-mount"} {
		t.Run(name, func(t *testing.T) {
			f := rolloutFixture()
			pod := obj(f.pods["items"].([]any)[0])
			spec := obj(pod["spec"])
			container := obj(spec["containers"].([]any)[0])
			init := obj(spec["initContainers"].([]any)[0])
			switch name {
			case "init-env":
				init["env"] = []any{map[string]any{"name": "PATH", "value": "/other"}}
			case "init-source-mount":
				obj(init["volumeMounts"].([]any)[0])["mountPath"] = "/elsewhere"
			case "alternate-path":
				obj(container["env"].([]any)[1])["value"] = "/other/config.json"
			case "nonlive":
				obj(container["env"].([]any)[0])["value"] = "simulated"
			case "wrong-source":
				obj(obj(spec["volumes"].([]any)[1])["secret"])["secretName"] = "other"
			case "writable":
				obj(container["volumeMounts"].([]any)[0])["readOnly"] = false
			case "wrong-copy":
				init["args"] = []any{"cp /other /live-private/config.json"}
			case "wrong-init-image":
				init["image"] = "other"
			case "wrong-image":
				container["image"] = "other"
			case "wrong-command":
				container["command"] = []any{"/other"}
			case "shadow-mount":
				container["volumeMounts"] = append(container["volumeMounts"].([]any), map[string]any{"name": "other", "mountPath": "/run/raptor/live/config.json"})
			case "extra-writer":
				spec["containers"] = append(spec["containers"].([]any), map[string]any{"name": "writer", "volumeMounts": []any{map[string]any{"name": "live-private", "mountPath": "/write"}}})
			case "disk-volume":
				obj(obj(spec["volumes"].([]any)[0])["emptyDir"])["medium"] = ""
			case "wrong-runtime-image":
				obj(obj(pod["status"])["containerStatuses"].([]any)[0])["imageID"] = "wrong"
			case "unfinished-init":
				obj(obj(pod["status"])["initContainerStatuses"].([]any)[0])["state"] = map[string]any{}
			case "deployment-path":
				obj(obj(obj(obj(f.deployment["spec"])["template"])["spec"])["containers"].([]any)[0])["command"] = []any{"/other"}
			}
			var out bytes.Buffer
			if run(context.Background(), []string{"verify", "--expected", "false", "--resource-version", "10", "--changed-after", "2026-10-16T01:00:00Z"}, f, &out) == nil {
				t.Fatal("unbound loaded config accepted")
			}
			if out.Len() != 0 {
				t.Fatal("rejected proof emitted output")
			}
		})
	}
}
