package resources

import (
	"context"
	"errors"
	"raptor-iap/infra-api/internal/domain"
	"testing"
)

type catalogFunc func(context.Context, string, string, string) (domain.ResourceContext, error)

func (f catalogFunc) Context(c context.Context, e, k, s string) (domain.ResourceContext, error) {
	return f(c, e, k, s)
}

type backendFunc func(context.Context, domain.ResourceContext) (domain.ResolvedResource, error)

func (f backendFunc) ResolveResource(c context.Context, r domain.ResourceContext) (domain.ResolvedResource, error) {
	return f(c, r)
}
func contextFixture(kind, code string) domain.ResourceContext {
	return domain.ResourceContext{EnvCode: "dev", Kind: kind, Code: code, Cloud: "aliyun", Region: "ap-southeast-1", AccountID: "123", ClusterID: "ack", RelatedResources: []domain.ResourceRef{}}
}
func TestResolverCatalogDenialBeforeProvider(t *testing.T) {
	for _, mode := range []string{"missing", "foreign", "cloud", "region", "cluster"} {
		s := NewResolver(catalogFunc(func(context.Context, string, string, string) (domain.ResourceContext, error) {
			c := contextFixture("db-proxy", "proxy")
			switch mode {
			case "missing":
				return c, errors.New("not found")
			case "foreign":
				c.EnvCode = "foreign"
			case "cloud":
				c.Cloud = "aws"
			case "region":
				c.Region = "cn-hangzhou"
			case "cluster":
				c.Kind = "application"
				c.ClusterID = ""
			}
			return c, nil
		}), backendFunc(func(context.Context, domain.ResourceContext) (domain.ResolvedResource, error) {
			t.Fatal("provider invoked before catalog denial")
			return domain.ResolvedResource{}, nil
		}))
		kind := "db-proxy"
		if mode == "cluster" {
			kind = "application"
		}
		if _, e := s.Resolve(context.Background(), "dev", kind, "proxy"); e == nil {
			t.Fatal(mode)
		}
	}
}
func proofService(appCount int) *Service {
	return NewResolver(catalogFunc(func(_ context.Context, _ string, k, c string) (domain.ResourceContext, error) {
		r := contextFixture(k, c)
		if k == "db-proxy" {
			r.RelatedResources = []domain.ResourceRef{{Kind: "application", Code: "app"}}
		}
		return r, nil
	}), backendFunc(func(_ context.Context, c domain.ResourceContext) (domain.ResolvedResource, error) {
		r := domain.ResolvedResource{Context: c, Environment: domain.Environment{Code: "dev", AccountID: "123", Region: "ap-southeast-1", ClusterID: "ack"}}
		switch c.Kind {
		case "db-proxy":
			r.Proxy = &domain.ProxyMapping{Code: "proxy", GroupID: "asg", ServerGroupID: "sgp", ListenerID: "lsn", Port: 6432, TargetDBCode: "db"}
		case "database":
			r.DatabaseWorkloads = []domain.DatabaseWorkload{{Type: "rds", ResourceID: "rds-actual", Code: "db"}}
		case "application":
			for i := 0; i < appCount; i++ {
				r.ApplicationWorkloads = append(r.ApplicationWorkloads, domain.ApplicationWorkload{Type: "kubernetes", WorkloadKey: "opaque", Target: domain.Target{EnvCode: "dev", AppCode: "app", ClusterID: "ack", Namespace: "ns", Name: "app", UID: "uid"}})
			}
		}
		return r, nil
	}))
}
func TestReplacementProofRequiresOneAppAndActualDatabase(t *testing.T) {
	for _, n := range []int{0, 2} {
		if _, e := proofService(n).ResolveProof(context.Background(), "dev", "proxy"); e == nil {
			t.Fatal("accepted app ambiguity", n)
		}
	}
	p, e := proofService(1).ResolveProof(context.Background(), "dev", "proxy")
	if e != nil || len(p.ResolutionDigest) != 64 || p.Identity.DatabaseResourceID != "rds-actual" || p.Identity.Application.UID != "uid" {
		t.Fatal(p, e)
	}
}
func TestProofDigestPinsImmutableTuple(t *testing.T) {
	p, e := proofService(1).ResolveProof(context.Background(), "dev", "proxy")
	if e != nil {
		t.Fatal(e)
	}
	if p.ResolutionDigest != "deb4e5bc24ac030fcf270b9bd8567683a03d1772ad5e068a6e88a86cb4bcd937" {
		t.Fatalf("canonical tuple changed: %s", p.ResolutionDigest)
	}
	for _, field := range []string{"listener", "port", "rds", "uid", "namespace", "name", "cluster"} {
		x := p.Identity
		switch field {
		case "listener":
			x.ListenerID = "new"
		case "port":
			x.Port++
		case "rds":
			x.DatabaseResourceID = "new"
		case "uid":
			x.Application.UID = "new"
		case "namespace":
			x.Application.Namespace = "new"
		case "name":
			x.Application.Name = "new"
		case "cluster":
			x.ClusterID = "new"
			x.Application.ClusterID = "new"
		}
		d, e := Digest(x)
		if e != nil || d == p.ResolutionDigest {
			t.Fatal("identity drift not pinned", field, e)
		}
	}
	d, e := Digest(p.Identity)
	if e != nil || d != p.ResolutionDigest {
		t.Fatal("unstable digest", e)
	}
}
func TestOrdinaryProxyReadAllowsMissingAppMapping(t *testing.T) {
	s := proofService(2)
	base := s.catalog
	s.catalog = catalogFunc(func(c context.Context, e, k, r string) (domain.ResourceContext, error) {
		v, x := base.Context(c, e, k, r)
		v.RelatedResources = []domain.ResourceRef{}
		return v, x
	})
	r, e := s.Resolve(context.Background(), "dev", "db-proxy", "proxy")
	if e != nil || r.Proxy == nil || r.ResolutionDigest != "" {
		t.Fatal(r, e)
	}
	if _, e = s.ResolveProof(context.Background(), "dev", "proxy"); e == nil {
		t.Fatal("proof accepted missing app")
	}
}

func TestProofRejectsChangedEnvironmentOrMissingDatabaseRealization(t *testing.T) {
	for _, mode := range []string{"cluster", "account", "database-missing", "database-ambiguous", "wrong-app"} {
		s := proofService(1)
		baseCatalog := s.catalog
		baseBackend := s.backend
		s.catalog = catalogFunc(func(ctx context.Context, e, k, c string) (domain.ResourceContext, error) {
			v, x := baseCatalog.Context(ctx, e, k, c)
			if k == "application" && mode == "cluster" {
				v.ClusterID = "foreign"
			}
			if k == "database" && mode == "account" {
				v.AccountID = "foreign"
			}
			return v, x
		})
		s.backend = backendFunc(func(ctx context.Context, c domain.ResourceContext) (domain.ResolvedResource, error) {
			r, e := baseBackend.ResolveResource(ctx, c)
			if c.Kind == "database" {
				switch mode {
				case "database-missing":
					r.DatabaseWorkloads = nil
				case "database-ambiguous":
					r.DatabaseWorkloads = append(r.DatabaseWorkloads, r.DatabaseWorkloads[0])
				}
			}
			if c.Kind == "application" && mode == "wrong-app" {
				r.ApplicationWorkloads[0].Target.AppCode = "unrelated"
			}
			return r, e
		})
		if _, e := s.ResolveProof(context.Background(), "dev", "proxy"); e == nil {
			t.Fatal("accepted changed proof", mode)
		}
	}
}

func TestProofRejectsForeignRelatedContextBeforeApplicationBackend(t *testing.T) {
	for _, field := range []string{"account", "cluster", "region", "canonical-code"} {
		t.Run(field, func(t *testing.T) {
			s := proofService(1)
			catalog, backend := s.catalog, s.backend
			s.catalog = catalogFunc(func(ctx context.Context, env, kind, code string) (domain.ResourceContext, error) {
				c, e := catalog.Context(ctx, env, kind, code)
				if kind == "application" {
					switch field {
					case "account":
						c.AccountID = "foreign"
					case "cluster":
						c.ClusterID = "foreign"
					case "region":
						c.Region = "cn-hangzhou"
					case "canonical-code":
						c.Code = "unrelated"
					}
				}
				return c, e
			})
			s.backend = backendFunc(func(ctx context.Context, c domain.ResourceContext) (domain.ResolvedResource, error) {
				if c.Kind == "application" {
					t.Fatal("foreign related catalog context reached application backend")
				}
				return backend.ResolveResource(ctx, c)
			})
			if _, e := s.ResolveProof(context.Background(), "dev", "proxy"); e == nil {
				t.Fatal("foreign related context admitted")
			}
		})
	}
}

func TestOrdinaryApplicationAliasResolvesCanonicalWorkload(t *testing.T) {
	s := proofService(1)
	catalog, backend := s.catalog, s.backend
	s.catalog = catalogFunc(func(ctx context.Context, env, kind, code string) (domain.ResourceContext, error) {
		if kind != "application" || code != "application-alias" {
			t.Fatalf("unexpected selector: %s %s", kind, code)
		}
		return catalog.Context(ctx, env, kind, "app")
	})
	s.backend = backendFunc(func(ctx context.Context, c domain.ResourceContext) (domain.ResolvedResource, error) {
		if c.Code != "app" {
			t.Fatal("backend did not receive canonical app code")
		}
		return backend.ResolveResource(ctx, c)
	})
	r, e := s.Resolve(context.Background(), "dev", "application", "application-alias")
	if e != nil || r.Context.Code != "app" || len(r.ApplicationWorkloads) != 1 || r.ApplicationWorkloads[0].Target.AppCode != "app" {
		t.Fatal(r, e)
	}
}

func TestProofUsesSingleValidatedRelatedCatalogContext(t *testing.T) {
	s := proofService(1)
	catalog, backend := s.catalog, s.backend
	applicationReads := 0
	s.catalog = catalogFunc(func(ctx context.Context, env, kind, code string) (domain.ResourceContext, error) {
		if kind == "application" {
			applicationReads++
			if applicationReads > 1 {
				t.Fatal("application context was re-read after validation")
			}
		}
		return catalog.Context(ctx, env, kind, code)
	})
	s.backend = backendFunc(func(ctx context.Context, c domain.ResourceContext) (domain.ResolvedResource, error) {
		if c.Kind == "application" && (applicationReads != 1 || c.EnvCode != "dev" || c.AccountID != "123" || c.ClusterID != "ack" || c.Region != "ap-southeast-1" || c.Code != "app") {
			t.Fatal("backend context differs from validated related identity")
		}
		return backend.ResolveResource(ctx, c)
	})
	p, e := s.ResolveProof(context.Background(), "dev", "proxy")
	if e != nil || applicationReads != 1 || p.Identity.Application.AppCode != "app" {
		t.Fatal(p, e, applicationReads)
	}
}
