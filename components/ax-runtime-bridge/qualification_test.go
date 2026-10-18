package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Wire shape emitted by the Gateway for the owner-approved read-only profile.
const qualificationWire = `{"binding":{"requestId":"aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa","attemptId":"bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb","operation":"db-proxy.replace-nodes","objectKind":"db-proxy","objectCode":"DP00001","envCode":"rdev.ali","skillsCommit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","definitionSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","clusterId":"platform-ack","model":"gpt-6-luna","providerId":"codex","connectionVersion":1,"executionMode":"qualification"}}`

func qualificationRequest(t *testing.T) Request {
	t.Helper()
	var r Request
	d := json.NewDecoder(strings.NewReader(qualificationWire))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		t.Fatal("Gateway qualification wire rejected", err)
	}
	r.Deadline = time.Now().Add(time.Minute)
	return r
}
func TestQualificationWireAcceptedForCleanup(t *testing.T) {
	r := qualificationRequest(t)
	if err := r.validate(true); err != nil {
		t.Fatal("qualification cleanup rejected", err)
	}
}
func TestReplacementWireAcceptedForLifecycle(t *testing.T) {
	r := qualificationRequest(t)
	r.Binding.ExecutionMode = "replacement"
	if err := r.validate(true); err != nil {
		t.Fatalf("replacement creation rejected: %v", err)
	}
	if err := r.validate(false); err != nil {
		t.Fatalf("replacement cleanup rejected: %v", err)
	}
}
func TestQualificationBindingCannotBroadenOtherOperations(t *testing.T) {
	for _, change := range []func(*Request){
		func(r *Request) { r.Binding.Operation = "application.question"; r.Binding.ObjectKind = "application" },
		func(r *Request) { r.Binding.Operation = "repository.change"; r.Binding.ObjectKind = "application" },
		func(r *Request) { r.Binding.ObjectKind = "application" },
		func(r *Request) { r.Binding.EnvCode = "prod.ali" },
		func(r *Request) { r.Binding.ProviderID = ""; r.Binding.ConnectionVersion = 0 },
		func(r *Request) { r.Repositories = []Repository{{}} },
	} {
		r := qualificationRequest(t)
		change(&r)
		if r.validate(false) == nil {
			t.Fatal("foreign qualification binding accepted")
		}
	}
	for _, mode := range []string{"", "unknown"} {
		r := qualificationRequest(t)
		raw, _ := json.Marshal(r)
		var wire map[string]any
		json.Unmarshal(raw, &wire)
		wire["binding"].(map[string]any)["executionMode"] = mode
		raw, _ = json.Marshal(wire)
		json.Unmarshal(raw, &r)
		if r.validate(false) == nil {
			t.Fatal("non-qualification profile accepted", mode)
		}
	}
}
func TestReplacementBindingStaysScoped(t *testing.T) {
	for _, change := range []func(*Request){
		func(r *Request) { r.Binding.ObjectKind = "application" },
		func(r *Request) { r.Binding.Operation = "application.question" },
		func(r *Request) { r.Binding.EnvCode = "prod.ali" },
		func(r *Request) { r.Binding.ProviderID = ""; r.Binding.ConnectionVersion = 0 },
		func(r *Request) { r.Repositories = []Repository{{}} },
	} {
		r := qualificationRequest(t)
		r.Binding.ExecutionMode = "replacement"
		change(&r)
		if r.validate(false) == nil {
			t.Fatal("foreign replacement binding accepted")
		}
	}
}
func TestQualificationModeChangesOwnershipSeal(t *testing.T) {
	r := qualificationRequest(t)
	before := r.seal()
	raw, _ := json.Marshal(r)
	var wire map[string]any
	json.Unmarshal(raw, &wire)
	wire["binding"].(map[string]any)["executionMode"] = "replacement"
	raw, _ = json.Marshal(wire)
	json.Unmarshal(raw, &r)
	if r.seal() == before {
		t.Fatal("mode absent from ownership seal")
	}
}
