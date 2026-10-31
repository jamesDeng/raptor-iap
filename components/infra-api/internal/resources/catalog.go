// Package resources resolves logical catalog resources without trusting caller cloud targets.
package resources

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"raptor-iap/infra-api/internal/domain"
	"regexp"
	"strings"
	"time"
)

type Catalog interface {
	Context(context.Context, string, string, string) (domain.ResourceContext, error)
}
type httpCatalog struct {
	origin         string
	user, password string
	client         http.Client
}

var logicalCode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

func selectors(env, kind, code string) bool {
	return logicalCode.MatchString(env) && logicalCode.MatchString(code) && (kind == "db-proxy" || kind == "application" || kind == "database")
}
func NewCatalog(origin, user, password string, client *http.Client) (Catalog, error) {
	u, e := url.Parse(origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || user == "" || password == "" {
		return nil, domain.ErrNotConfigured
	}
	c := http.Client{}
	if client != nil {
		c = *client
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if c.Timeout <= 0 || c.Timeout > 10*time.Second {
		c.Timeout = 10 * time.Second
	}
	return &httpCatalog{origin: strings.TrimSuffix(origin, "/"), user: user, password: password, client: c}, nil
}
func (c *httpCatalog) Context(ctx context.Context, env, kind, code string) (domain.ResourceContext, error) {
	if !selectors(env, kind, code) {
		return domain.ResourceContext{}, domain.ErrScope
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	q := url.Values{"envCode": {env}, "kind": {kind}, "code": {code}}
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, c.origin+"/v1/infra/resource-context?"+q.Encode(), nil)
	if e != nil {
		return domain.ResourceContext{}, domain.ErrScope
	}
	r.SetBasicAuth(c.user, c.password)
	r.Header.Set("Accept", "application/json")
	resp, e := c.client.Do(r)
	if e != nil {
		return domain.ResourceContext{}, errors.New("catalog unavailable")
	}
	defer resp.Body.Close()
	typ, _, e := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if e != nil || typ != "application/json" || resp.StatusCode != 200 {
		return domain.ResourceContext{}, errors.New("catalog context refused")
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if e != nil || len(body) > 1<<20 {
		return domain.ResourceContext{}, errors.New("invalid catalog response")
	}
	if e = uniqueJSON(body); e != nil {
		return domain.ResourceContext{}, errors.New("invalid catalog response")
	}
	var projection struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &projection) != nil || len(projection.Data) != 8 {
		return domain.ResourceContext{}, errors.New("invalid catalog projection")
	}
	for _, key := range []string{"envCode", "kind", "code", "cloud", "region", "accountId", "clusterId", "relatedResources"} {
		if raw, ok := projection.Data[key]; !ok || string(raw) == "null" {
			return domain.ResourceContext{}, errors.New("missing catalog field")
		}
	}
	var envelope struct {
		Data *domain.ResourceContext `json:"data"`
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&envelope); e != nil || envelope.Data == nil {
		return domain.ResourceContext{}, errors.New("invalid catalog context")
	}
	v := *envelope.Data
	if e = validateContext(v, env, kind); e != nil {
		return domain.ResourceContext{}, e
	}
	return v, nil
}
func validateContext(c domain.ResourceContext, env, kind string) error {
	if c.EnvCode != env || c.Kind != kind || !selectors(c.EnvCode, c.Kind, c.Code) || c.AccountID == "" || c.Cloud != "aliyun" || c.Region != "ap-southeast-1" || c.RelatedResources == nil {
		return domain.ErrScope
	}
	if c.Kind == "application" && c.ClusterID == "" {
		return domain.ErrScope
	}
	if c.Kind != "db-proxy" && len(c.RelatedResources) > 0 || len(c.RelatedResources) > 1 {
		return domain.ErrScope
	}
	for _, r := range c.RelatedResources {
		if r.Kind != "application" || !logicalCode.MatchString(r.Code) {
			return domain.ErrScope
		}
	}
	return nil
}

// Reject duplicate JSON keys before typed decoding, including nested objects.
func uniqueJSON(body []byte) error {
	d := json.NewDecoder(strings.NewReader(string(body)))
	var value func() error
	value = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		del, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch del {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				s, ok := k.(string)
				if !ok || seen[s] {
					return errors.New("duplicate key")
				}
				seen[s] = true
				if e = value(); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e = value(); e != nil {
					return e
				}
			}
		default:
			return errors.New("invalid delimiter")
		}
		_, e = d.Token()
		return e
	}
	if e := value(); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
