package main

import (
	"net/http"
	"raptor-iap/infra-api/internal/api"
	"raptor-iap/infra-api/internal/approval"
	"raptor-iap/infra-api/internal/commands"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/metrics"
	"raptor-iap/infra-api/internal/provider"
	"reflect"
)

type runtimeOptions struct{ Restart, Proxy bool }
type runtimeDependencies struct {
	AgentCheck api.AgentCheck
	Backend    commands.Backend
	Metrics    commands.Metrics
	Approval   commands.Approval
	Claims     commands.Claims
	Fleet      commands.Fleet
}

func absent(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Func, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func liveDependencies(scopes map[string]domain.Environment, opts runtimeOptions, origin, user, password string) (runtimeDependencies, error) {
	if !opts.Proxy {
		return runtimeDependencies{}, nil
	}
	a, e := approval.New(origin, user, password, nil)
	if e != nil {
		return runtimeDependencies{}, domain.ErrNotConfigured
	}
	b, e := provider.NewProxyBackend()
	if e != nil {
		return runtimeDependencies{}, domain.ErrNotConfigured
	}
	q, e := provider.NewPrometheusQuery()
	if e != nil {
		return runtimeDependencies{}, domain.ErrNotConfigured
	}
	m, e := metrics.New(scopes, q)
	if e != nil {
		return runtimeDependencies{}, domain.ErrNotConfigured
	}
	return runtimeDependencies{Backend: b, Metrics: m, Approval: a, Claims: a, Fleet: a}, nil
}
func buildRuntimeHandler(reader api.Reader, scopes map[string]domain.Environment, user, password, header string, opts runtimeOptions, deps runtimeDependencies) (http.Handler, error) {
	if !opts.Proxy {
		return buildHandlerWithAgent(reader, scopes, user, password, header, opts.Restart, deps.AgentCheck)
	}
	if absent(deps.Backend) || absent(deps.Metrics) || absent(deps.Approval) || absent(deps.Claims) || absent(deps.Fleet) {
		return nil, domain.ErrNotConfigured
	}
	mappings := 0
	for _, env := range scopes {
		for _, p := range env.Proxies {
			if env.Region != "ap-southeast-1" || env.ClusterID == "" || env.AccountID == "" || p.Code == "" || p.GroupID == "" || p.ServerGroupID == "" || p.ListenerID == "" || p.TargetDBCode == "" || p.Port < 1 || p.Port > 65535 {
				return nil, domain.ErrNotConfigured
			}
			mappings++
		}
	}
	if mappings == 0 {
		return nil, domain.ErrNotConfigured
	}
	runtime := commands.Runtime{Backend: deps.Backend, Metrics: deps.Metrics, Approval: deps.Approval, Claims: deps.Claims, Fleet: deps.Fleet, EvidenceMode: "live"}
	paths := []string{"/v1/db-proxy-scale", "/v1/db-proxy-node-protection", "/v1/db-proxy-node-deregistration"}
	if opts.Restart {
		r, ok := reader.(restarter)
		if !ok || absent(r) {
			return nil, domain.ErrNotConfigured
		}
		runtime.Restarter = r
		paths = append(paths, "/v1/deployment-restart")
	}
	return api.NewWithAgentCommands(reader, scopes, user, password, header, runtime, api.NewServiceAuthorizer(user, paths...), deps.AgentCheck)
}
