package policy

import (
	"testing"
	"time"
)

// Counting unhealthy nodes or accepting equality would permit unsafe removal.
func TestDeregistrationRequiresHealthyMajorityOfDesired(t *testing.T) {
	for _, tc := range []struct {
		desired int
		healthy []bool
		remove  []string
		want    string
	}{
		{4, []bool{true, true, true, true}, []string{"a", "b"}, "InsufficientHealthyCapacity"},
		{4, []bool{true, true, true, true}, []string{"a"}, ""},
		{3, []bool{true, true, true}, []string{"a"}, ""},
		{4, []bool{true, true, true, false}, []string{"a"}, "InsufficientHealthyCapacity"},
		{4, []bool{true, true, true, true}, []string{"foreign"}, "ScopeMismatch"},
	} {
		s := ProxySnapshot{EnvCode: "dev", ProxyCode: "p", GroupID: "g", ServerGroupID: "backend", Desired: tc.desired}
		for i, h := range tc.healthy {
			s.Nodes = append(s.Nodes, Node{ID: string(rune('a' + i)), Registered: true, Healthy: h})
		}
		e := DeregistrationAllowed(s, "backend", tc.remove)
		if code(e) != tc.want {
			t.Fatalf("%+v: %v", tc, e)
		}
	}
}

// Idle clients must block scale-in just as running clients do; protection only
// exempts protected members, never an arbitrary predicted termination target.
func TestScaleInRequiresFreshZeroClientsOnEveryUnprotectedNode(t *testing.T) {
	now := time.Unix(1000, 0)
	s := ProxySnapshot{Nodes: []Node{{ID: "a"}, {ID: "b"}, {ID: "protected", Protected: true}}}
	for _, tc := range []struct {
		rows []ClientObservation
		want string
	}{
		{[]ClientObservation{{"a", 0, now}, {"b", 0, now}}, ""},
		{[]ClientObservation{{"a", 0, now}, {"b", 1, now}}, "ConnectedClientsPresent"},
		{[]ClientObservation{{"a", 0, now}}, "MetricsInvalid"},
		{[]ClientObservation{{"a", 0, now}, {"a", 0, now}, {"b", 0, now}}, "MetricsInvalid"},
		{[]ClientObservation{{"a", 0, now.Add(-46 * time.Second)}, {"b", 0, now}}, "MetricsInvalid"},
		{[]ClientObservation{{"a", 0, now.Add(6 * time.Second)}, {"b", 0, now}}, "MetricsInvalid"},
		{[]ClientObservation{{"a", 0.5, now}, {"b", 0, now}}, "MetricsInvalid"},
	} {
		if e := ScaleInClientsAllowed(s, tc.rows, now); code(e) != tc.want {
			t.Fatalf("%+v: %v", tc, e)
		}
	}
}
func TestProxyResolutionNeverSelectsAmbiguousOrForeignGroup(t *testing.T) {
	groups := []ProxySnapshot{{EnvCode: "dev", ProxyCode: "p", GroupID: "g"}, {EnvCode: "dev", ProxyCode: "p", GroupID: "other"}}
	if _, e := ResolveProxy(groups, "dev", "p", "g"); code(e) != "AmbiguousTarget" {
		t.Fatal(e)
	}
	if _, e := ResolveProxy(groups[:1], "dev", "p", "foreign"); code(e) != "IdentityChanged" {
		t.Fatal(e)
	}
	if _, e := ResolveProxy(groups, "foreign", "p", "g"); code(e) != "TargetNotFound" {
		t.Fatal(e)
	}
}
func code(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}
