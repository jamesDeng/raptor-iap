package backend

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
)

type contextInfraSpy struct{ calls int }

func (s *contextInfraSpy) ListDeployments(context.Context, domain.Environment, domain.Object) ([]adapters.Deployment, error) {
	s.calls++
	return nil, domain.ErrUnavailable
}

// Exercises actual service registration/auth/projection, not a standalone handler seam.
func TestInfraResourceContextStrictServiceRoute(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if err := db.Migrate(ctx, p); err != nil {
		t.Fatal(err)
	}
	s := New(p)
	infra := &contextInfraSpy{}
	s.Catalog.Infra = infra
	if err := s.Catalog.CreateGroup(ctx, "fixture", "Fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "application", Name: "Fixture"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Catalog.SaveEnvironment(ctx, domain.Environment{Code: "rdev.ali", GroupCode: "fixture", Stage: "dev", Cloud: "aliyun", Region: "ap-southeast-1", AccountID: "fixture-account", ACKClusterID: "fixture-cluster", Config: map[string]any{"privateSetting": "do-not-expose", "infraApiUrl": "https://private.example"}}, false); err != nil {
		t.Fatal(err)
	}
	s.RegisterService("fixture-service", "fixture-password")
	valid := "envCode=rdev.ali&kind=application&code=A00001"
	for _, tc := range []struct {
		name, method, query, auth string
		status                    int
	}{
		{"valid", "GET", valid, "service", 200},
		{"unauthenticated", "GET", valid, "", 401},
		{"wrong-password", "GET", valid, "wrong", 401},
		{"browser-cookie", "GET", valid, "browser", 401},
		{"missing-env", "GET", "kind=application&code=A00001", "service", 400},
		{"missing-kind", "GET", "envCode=rdev.ali&code=A00001", "service", 400},
		{"missing-code", "GET", "envCode=rdev.ali&kind=application", "service", 400},
		{"empty-code", "GET", "envCode=rdev.ali&kind=application&code=", "service", 400},
		{"duplicate-env", "GET", valid + "&envCode=rdev.ali", "service", 400},
		{"duplicate-kind", "GET", valid + "&kind=application", "service", 400},
		{"duplicate-code", "GET", valid + "&code=A00001", "service", 400},
		{"provider-selector", "GET", valid + "&clusterId=foreign", "service", 400},
		{"endpoint-selector", "GET", valid + "&infraApiUrl=https://foreign.example", "service", 400},
		{"unknown-query", "GET", valid + "&query=up", "service", 400},
		{"malformed-query", "GET", valid + "&bad=%ZZ", "service", 400},
		{"semicolon-query", "GET", valid + ";hidden=x", "service", 400},
		{"whitespace", "GET", "envCode=rdev.ali&kind=application&code=%20A00001", "service", 400},
		{"foreign-kind", "GET", "envCode=rdev.ali&kind=database&code=A00001", "service", 404},
		{"foreign-env", "GET", "envCode=foreign&kind=application&code=A00001", "service", 404},
		{"unsupported-kind", "GET", "envCode=rdev.ali&kind=ess&code=A00001", "service", 400},
		{"post", "POST", valid, "service", 405},
		{"head", "HEAD", valid, "service", 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/v1/infra/resource-context?"+tc.query, nil)
			switch tc.auth {
			case "service":
				r.SetBasicAuth("fixture-service", "fixture-password")
			case "wrong":
				r.SetBasicAuth("fixture-service", "wrong")
			case "browser":
				r.Header.Set("Cookie", "raptor_session=fixture")
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if tc.status == 200 {
				var got map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				want := map[string]any{"data": map[string]any{"envCode": "rdev.ali", "kind": "application", "code": "A00001", "cloud": "aliyun", "region": "ap-southeast-1", "accountId": "fixture-account", "clusterId": "fixture-cluster", "relatedResources": []any{}}}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("unexpected projection: %s", w.Body.String())
				}
			}
		})
	}
	if infra.calls != 0 {
		t.Fatalf("resource context called Infra %d times", infra.calls)
	}
}
