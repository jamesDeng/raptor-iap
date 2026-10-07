package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	origin string
	http   *http.Client
}

func New(origin string, client *http.Client) (*Client, error) {
	u, e := url.Parse(origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid metrics origin")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{strings.TrimSuffix(origin, "/"), &c}, nil
}
func fail(code string) error { return &domain.CommandError{Code: code} }

type vector struct {
	Metric map[string]string `json:"metric"`
	Value  []json.RawMessage `json:"value"`
}

func (c *Client) query(ctx context.Context, name string, s policy.ProxySnapshot) (map[string]float64, error) {
	q := fmt.Sprintf(`%s{env_code=%s,proxy_code=%s,ess_group_id=%s,ecs_instance_id=~".+"}`, name, strconv.Quote(s.EnvCode), strconv.Quote(s.ProxyCode), strconv.Quote(s.GroupID))
	request, e := http.NewRequestWithContext(ctx, "GET", c.origin+"/api/v1/query?"+url.Values{"query": {q}}.Encode(), nil)
	if e != nil {
		return nil, fail("MetricsUnavailable")
	}
	response, e := c.http.Do(request)
	if e != nil {
		return nil, fail("MetricsUnavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fail("MetricsUnavailable")
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 1048577))
	if e != nil || len(raw) > 1048576 {
		return nil, fail("MetricsUnavailable")
	}
	var out struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string   `json:"resultType"`
			Result     []vector `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Status != "success" || out.Data.ResultType != "vector" {
		return nil, fail("MetricsInvalid")
	}
	members := map[string]bool{}
	for _, n := range s.Nodes {
		members[n.ID] = true
	}
	values := map[string]float64{}
	for _, r := range out.Data.Result {
		id := r.Metric["ecs_instance_id"]
		if id == "" || !members[id] || r.Metric["env_code"] != s.EnvCode || r.Metric["proxy_code"] != s.ProxyCode || r.Metric["ess_group_id"] != s.GroupID || len(r.Value) != 2 {
			return nil, fail("MetricsInvalid")
		}
		if _, exists := values[id]; exists {
			return nil, fail("MetricsInvalid")
		}
		var text string
		if json.Unmarshal(r.Value[1], &text) != nil {
			return nil, fail("MetricsInvalid")
		}
		value, e := strconv.ParseFloat(text, 64)
		if e != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return nil, fail("MetricsInvalid")
		}
		values[id] = value
	}
	return values, nil
}
func (c *Client) ConnectedClients(ctx context.Context, s policy.ProxySnapshot) ([]policy.ClientObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	counts, e := c.query(ctx, "raptor_pgcat_connected_clients", s)
	if e != nil {
		return nil, e
	}
	observed, e := c.query(ctx, "raptor_pgcat_connected_clients_observed_at_seconds", s)
	if e != nil {
		return nil, e
	}
	rows := []policy.ClientObservation{}
	for _, n := range s.Nodes {
		if n.Protected {
			continue
		}
		count, ok := counts[n.ID]
		stamp, present := observed[n.ID]
		if !ok || !present || math.Trunc(count) != count || stamp <= 0 || stamp >= float64(math.MaxInt64) {
			return nil, fail("MetricsInvalid")
		}
		seconds, fraction := math.Modf(stamp)
		rows = append(rows, policy.ClientObservation{InstanceID: n.ID, Clients: count, ObservedAt: time.Unix(int64(seconds), int64(fraction*1e9))})
	}
	return rows, nil
}
