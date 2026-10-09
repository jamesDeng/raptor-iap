// Package approval verifies exact Raptor bindings. Check is not an atomic
// consume/claim and must not be treated as replay protection.
package approval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"raptor-iap/infra-api/internal/jsoninput"
	"strings"
	"time"
)

type Binding struct {
	RequestID, ActionID, EnvCode, ProxyCode, GroupID string
	DesiredCapacity                                  int
}

func (b Binding) Wire() any {
	return struct {
		RequestID string `json:"requestId"`
		ActionID  string `json:"actionId"`
		Interface string `json:"interface"`
		EnvCode   string `json:"envCode"`
		Target    struct {
			ProxyCode string `json:"proxyCode"`
			GroupID   string `json:"groupId"`
		} `json:"target"`
		Parameters struct {
			DesiredCapacity int `json:"desiredCapacity"`
		} `json:"parameters"`
	}{RequestID: b.RequestID, ActionID: b.ActionID, Interface: "ess.scale-in", EnvCode: b.EnvCode, Target: struct {
		ProxyCode string `json:"proxyCode"`
		GroupID   string `json:"groupId"`
	}{b.ProxyCode, b.GroupID}, Parameters: struct {
		DesiredCapacity int `json:"desiredCapacity"`
	}{b.DesiredCapacity}}
}

type Client struct {
	origin, user, password string
	http                   *http.Client
}

func New(origin, user, password string, client *http.Client) (*Client, error) {
	u, e := url.Parse(origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || user == "" || strings.Contains(user, ":") || password == "" {
		return nil, errors.New("invalid approval configuration")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{strings.TrimSuffix(origin, "/"), user, password, &copy}, nil
}
func (c *Client) Check(ctx context.Context, b Binding) (bool, error) {
	if b.RequestID == "" || b.ActionID == "" || b.EnvCode == "" || b.ProxyCode == "" || b.GroupID == "" || b.DesiredCapacity < 0 {
		return false, errors.New("invalid approval binding")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	raw, e := json.Marshal(b.Wire())
	if e != nil {
		return false, errors.New("approval unavailable")
	}
	req, e := http.NewRequestWithContext(ctx, "POST", c.origin+"/v1/approval-check", bytes.NewReader(raw))
	if e != nil {
		return false, errors.New("approval unavailable")
	}
	req.SetBasicAuth(c.user, c.password)
	req.Header.Set("Content-Type", "application/json")
	response, e := c.http.Do(req)
	if e != nil {
		return false, errors.New("approval unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return false, errors.New("approval unavailable")
	}
	raw, e = io.ReadAll(io.LimitReader(response.Body, 8193))
	if e != nil {
		return false, errors.New("approval unavailable")
	}
	var out struct {
		Data struct {
			Allowed bool   `json:"allowed"`
			Reason  string `json:"reason"`
		} `json:"data"`
	}
	if jsoninput.Decode(raw, &out) != nil {
		return false, errors.New("approval unavailable")
	}
	return out.Data.Allowed, nil
}
