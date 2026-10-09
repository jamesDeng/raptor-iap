package raptor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/config"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrLogin = errors.New("NeedsLogin")
var ErrResponse = errors.New("InvalidRaptorResponse")
var ErrUnavailable = errors.New("RaptorUnavailable")

type Client struct {
	cfg    config.Config
	http   *http.Client
	mu     sync.RWMutex
	csrf   string
	userID string
}

func NewClient(c config.Config) (*Client, error) {
	if e := c.Validate(); e != nil {
		return nil, e
	}
	jar, _ := cookiejar.New(nil)
	return &Client{cfg: c, http: &http.Client{Jar: jar, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Login(ctx context.Context) (string, error) {
	b, e := config.PrivateRead(c.cfg.CredentialsFile)
	if e != nil {
		return "", e
	}
	var auth struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.Unmarshal(b, &auth) != nil || auth.Username == "" || auth.Password == "" {
		return "", config.ErrConfig
	}
	var out struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		CSRF string `json:"csrf"`
	}
	payload, _ := json.Marshal(auth)
	if e = c.call(ctx, "POST", "/api/v1/login", "", payload, &out); e != nil {
		return "", e
	}
	u, _ := url.Parse(c.cfg.Origin)
	found := false
	for _, v := range c.http.Jar.Cookies(u) {
		if v.Name == "raptor_session" && v.Value != "" {
			found = true
		}
	}
	if !found || out.User.ID == "" || out.CSRF == "" {
		return "", ErrResponse
	}
	c.mu.Lock()
	c.csrf = out.CSRF
	c.userID = out.User.ID
	c.mu.Unlock()
	return out.User.ID, nil
}
func (c *Client) call(ctx context.Context, method, p, key string, b []byte, out any) error {
	r, e := http.NewRequestWithContext(ctx, method, c.cfg.Origin+p, bytes.NewReader(b))
	if e != nil {
		return ErrResponse
	}
	r.Header.Set("Accept", "application/json")
	if b != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	c.mu.RLock()
	r.Header.Set("X-CSRF-Token", c.csrf)
	c.mu.RUnlock()
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	res, e := c.http.Do(r)
	if e != nil {
		return ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return ErrLogin
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ErrUnavailable
	}
	body, e := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if e != nil || len(body) > 1<<20 || json.Unmarshal(body, &envelope) != nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, out) != nil {
		return ErrResponse
	}
	return nil
}
func (c *Client) Create(ctx context.Context, key string, payload json.RawMessage) (Request, error) {
	var out Request
	e := c.call(ctx, "POST", "/api/v1/requests", key, payload, &out)
	if e == nil && out.ID == "" {
		e = ErrResponse
	}
	return out, e
}
func (c *Client) View(ctx context.Context, id string) (RequestView, error) {
	var out RequestView
	e := c.call(ctx, "GET", "/api/v1/requests/"+url.PathEscape(id), "", nil, &out)
	if e == nil && out.Request.ID != id {
		e = ErrResponse
	}
	return out, e
}
func (c *Client) Events(ctx context.Context, id string, after int64) (Timeline, error) {
	var out Timeline
	e := c.call(ctx, "GET", "/api/v1/requests/"+url.PathEscape(id)+"/events?after="+strconv.FormatInt(after, 10), "", nil, &out)
	return out, e
}
func (c *Client) Cancel(ctx context.Context, id string) error {
	var out struct {
		Accepted bool `json:"accepted"`
	}
	e := c.call(ctx, "POST", "/api/v1/requests/"+url.PathEscape(id)+"/actions", "", []byte(`{"action":"cancel","instructions":""}`), &out)
	if e == nil && !out.Accepted {
		return ErrResponse
	}
	return e
}
func QuestionPayload(c config.Config, text string) (json.RawMessage, error) {
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > 2000 || len(text) > 8192 {
		return nil, errors.New("InvalidQuestion")
	}
	return json.Marshal(Definition{Type: "agent", Model: c.Model, Object: ObjectRef{Kind: "application", Code: c.ApplicationCode}, EnvCode: c.EnvironmentCode, Operations: []Operation{{Name: "application.question", Parameters: map[string]string{"question": text}}}})
}
