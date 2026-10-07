// Package policy implements the accepted per-command checks. It never chooses
// a replacement sequence or grants approval. Snapshots must be complete and
// freshly resolved by an authoritative provider adapter.
package policy

import (
	"math"
	"raptor-iap/infra-api/internal/domain"
	"time"
)

type Node struct {
	ID                             string
	Protected, Registered, Healthy bool
}
type ProxySnapshot struct {
	EnvCode, ProxyCode, GroupID, ServerGroupID string
	Desired, Min, Max                          int
	Nodes                                      []Node
}
type ClientObservation struct {
	InstanceID string
	Clients    float64
	ObservedAt time.Time
}

func refuse(code string) error { return &domain.CommandError{Code: code} }
func ResolveProxy(groups []ProxySnapshot, env, proxy, pin string) (ProxySnapshot, error) {
	matches := []ProxySnapshot{}
	for _, g := range groups {
		if g.EnvCode == env && g.ProxyCode == proxy {
			matches = append(matches, g)
		}
	}
	if len(matches) == 0 {
		return ProxySnapshot{}, refuse("TargetNotFound")
	}
	if len(matches) != 1 {
		return ProxySnapshot{}, refuse("AmbiguousTarget")
	}
	if matches[0].GroupID != pin {
		return ProxySnapshot{}, refuse("IdentityChanged")
	}
	return matches[0], nil
}
func Membership(s ProxySnapshot, ids []string) error {
	if len(ids) == 0 {
		return refuse("ScopeMismatch")
	}
	members := map[string]bool{}
	for _, n := range s.Nodes {
		if n.ID == "" || members[n.ID] {
			return refuse("TargetChanged")
		}
		members[n.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !members[id] || seen[id] {
			return refuse("ScopeMismatch")
		}
		seen[id] = true
	}
	return nil
}
func DeregistrationAllowed(s ProxySnapshot, backend string, ids []string) error {
	if s.ServerGroupID == "" || s.ServerGroupID != backend {
		return refuse("ScopeMismatch")
	}
	if s.Desired < 0 {
		return refuse("TargetChanged")
	}
	if e := Membership(s, ids); e != nil {
		return e
	}
	removed := map[string]bool{}
	for _, id := range ids {
		removed[id] = true
	}
	remaining := 0
	for _, n := range s.Nodes {
		if n.Registered && n.Healthy && !removed[n.ID] {
			remaining++
		}
	}
	// Division avoids overflowing 2*remaining while retaining strict majority.
	if remaining <= s.Desired/2 {
		return refuse("InsufficientHealthyCapacity")
	}
	return nil
}
func ScaleInClientsAllowed(s ProxySnapshot, rows []ClientObservation, now time.Time) error {
	observed := map[string]ClientObservation{}
	for _, r := range rows {
		if _, exists := observed[r.InstanceID]; exists {
			return refuse("MetricsInvalid")
		}
		observed[r.InstanceID] = r
	}
	eligible := 0
	members := map[string]bool{}
	for _, n := range s.Nodes {
		if n.ID == "" || members[n.ID] {
			return refuse("TargetChanged")
		}
		members[n.ID] = true
		if n.Protected {
			continue
		}
		eligible++
		r, ok := observed[n.ID]
		if !ok || math.IsNaN(r.Clients) || math.IsInf(r.Clients, 0) || r.Clients < 0 || math.Trunc(r.Clients) != r.Clients || r.ObservedAt.IsZero() || now.Sub(r.ObservedAt) > 45*time.Second || r.ObservedAt.Sub(now) > 5*time.Second {
			return refuse("MetricsInvalid")
		}
		if r.Clients != 0 {
			return refuse("ConnectedClientsPresent")
		}
	}
	if eligible == 0 {
		return refuse("NoEligibleNodes")
	}
	return nil
}
func ScaleDirection(s ProxySnapshot, desired int) (string, error) {
	if s.Min < 0 || s.Max < s.Min || s.Desired < s.Min || s.Desired > s.Max {
		return "", refuse("TargetChanged")
	}
	if desired < s.Min || desired > s.Max {
		return "", refuse("InvalidCapacity")
	}
	if desired == s.Desired {
		return "no_change", nil
	}
	if desired > s.Desired {
		return "out", nil
	}
	return "in", nil
}
