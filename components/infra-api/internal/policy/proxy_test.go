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

// Only a complete dedicated cohort may retain an equal, protected healthy pair.
func TestDeregistrationAllowsOnlyCompleteProtectedReplacementCapacity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ProxySnapshot)
		ids    []string
		want   string
	}{
		{name: "batch originals", ids: []string{"old-a", "old-b"}},
		{name: "second sequential original", change: func(s *ProxySnapshot) { s.Nodes[0].Registered = false }, ids: []string{"old-b"}},
		{name: "before expansion", change: func(s *ProxySnapshot) { s.Desired = 2; s.Nodes = s.Nodes[:2] }, ids: []string{"old-a"}, want: "InsufficientHealthyCapacity"},
		{name: "one survivor", ids: []string{"old-a", "old-b", "new-a"}, want: "InsufficientHealthyCapacity"},
		{name: "unprotected survivor", change: func(s *ProxySnapshot) { s.Nodes[2].Protected = false }, ids: []string{"old-a", "old-b"}, want: "InsufficientHealthyCapacity"},
		{name: "unhealthy survivor", change: func(s *ProxySnapshot) { s.Nodes[2].Healthy = false }, ids: []string{"old-a", "old-b"}, want: "InsufficientHealthyCapacity"},
		{name: "unregistered survivor", change: func(s *ProxySnapshot) { s.Nodes[2].Registered = false }, ids: []string{"old-a", "old-b"}, want: "InsufficientHealthyCapacity"},
		{name: "wrong minimum", change: func(s *ProxySnapshot) { s.Min = 1 }, ids: []string{"old-a", "old-b"}, want: "InsufficientHealthyCapacity"},
		{name: "wrong maximum", change: func(s *ProxySnapshot) { s.Max = 5 }, ids: []string{"old-a", "old-b"}, want: "InsufficientHealthyCapacity"},
		{name: "wrong desired", change: func(s *ProxySnapshot) { s.Desired = 5; s.Max = 5 }, ids: []string{"old-a", "old-b"}, want: "InsufficientHealthyCapacity"},
		{name: "incomplete membership", change: func(s *ProxySnapshot) { s.Nodes = s.Nodes[1:] }, ids: []string{"old-b"}, want: "InsufficientHealthyCapacity"},
		{name: "extra member", change: func(s *ProxySnapshot) { s.Nodes = append(s.Nodes, Node{ID: "extra"}) }, ids: []string{"old-a", "old-b"}, want: "InsufficientHealthyCapacity"},
		{name: "foreign id", ids: []string{"foreign"}, want: "ScopeMismatch"},
		{name: "duplicate requested id", ids: []string{"old-a", "old-a"}, want: "ScopeMismatch"},
		{name: "duplicate member", change: func(s *ProxySnapshot) { s.Nodes[0].ID = "old-b" }, ids: []string{"old-b"}, want: "TargetChanged"},
		{name: "blank member", change: func(s *ProxySnapshot) { s.Nodes[0].ID = "" }, ids: []string{"old-b"}, want: "TargetChanged"},
		{name: "foreign backend", change: func(s *ProxySnapshot) { s.ServerGroupID = "other" }, ids: []string{"old-a", "old-b"}, want: "ScopeMismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ProxySnapshot{Desired: 4, Min: 2, Max: 4, ServerGroupID: "backend", Nodes: []Node{
				{ID: "old-a", Registered: true, Healthy: true}, {ID: "old-b", Registered: true, Healthy: true},
				{ID: "new-a", Protected: true, Registered: true, Healthy: true}, {ID: "new-b", Protected: true, Registered: true, Healthy: true},
			}}
			if tc.change != nil {
				tc.change(&s)
			}
			if e := DeregistrationAllowed(s, "backend", tc.ids); code(e) != tc.want {
				t.Fatalf("want %q, got %v", tc.want, e)
			}
		})
	}
}

func TestDirectReplacementScaleInGuardDoesNotChangeOtherCapacityPolicies(t *testing.T) {
	for _, tc := range []struct {
		name    string
		s       ProxySnapshot
		desired int
	}{
		{"other minimum", ProxySnapshot{Desired: 4, Min: 1, Max: 4}, 2},
		{"other maximum", ProxySnapshot{Desired: 4, Min: 2, Max: 5}, 2},
		{"other current capacity", ProxySnapshot{Desired: 3, Min: 2, Max: 4}, 2},
		{"staged scale-in", ProxySnapshot{Desired: 4, Min: 2, Max: 4}, 3},
		{"no change", ProxySnapshot{Desired: 4, Min: 2, Max: 4}, 4},
		{"scale-out", ProxySnapshot{Desired: 2, Min: 2, Max: 4}, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if e := DirectReplacementScaleInAllowed(tc.s, tc.desired); e != nil {
				t.Fatalf("unrelated capacity operation changed: %v", e)
			}
		})
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
