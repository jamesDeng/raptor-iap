package runtime

import (
	"context"
	"errors"
	"fmt"
	fc "github.com/alibabacloud-go/fcsandbox-20260509/client"
	"github.com/alibabacloud-go/tea/dara"
	"net/http"
	"time"
)

var ErrKeyCreateUnconfirmed = errors.New("KeyCreateUnconfirmed")
var ErrKeyCleanupUnconfirmed = errors.New("KeyCleanupUnconfirmed")
var ErrKeyInventoryUnavailable = errors.New("KeyInventoryUnavailable")

type ControlKey struct {
	ID    string `json:"id"`
	value string
}

func (k ControlKey) String() string { return "[control key " + k.ID + "]" }

type KeyInfo struct{ ID, Name, TeamID, AccountID string }
type ManagementAPI interface {
	CreateApiKeyWithOptions(*fc.CreateApiKeyRequest, map[string]*string, *dara.RuntimeOptions) (*fc.CreateApiKeyResponse, error)
	ListApiKeysWithOptions(*fc.ListApiKeysRequest, map[string]*string, *dara.RuntimeOptions) (*fc.ListApiKeysResponse, error)
	UpdateApiKeyWithOptions(*string, *fc.UpdateApiKeyRequest, map[string]*string, *dara.RuntimeOptions) (*fc.UpdateApiKeyResponse, error)
	DeleteApiKeyWithOptions(*string, *fc.DeleteApiKeyRequest, map[string]*string, *dara.RuntimeOptions) (*fc.DeleteApiKeyResponse, error)
}
type Management struct {
	API               ManagementAPI
	Client            *fc.Client
	TeamID, AccountID string
}
type guardedSDKHTTP struct {
	ctx    context.Context
	origin string
}

func (g guardedSDKHTTP) Call(r *http.Request, tr *http.Transport) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != g.origin {
		return nil, ErrConfiguration
	}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(r.WithContext(g.ctx))
	if e != nil {
		return nil, ErrRuntimeUnavailable
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		resp.Body.Close()
		return nil, ErrRuntimeUnavailable
	}
	return resp, nil
}
func (m Management) api(ctx context.Context) ManagementAPI {
	if m.Client != nil {
		client := *m.Client
		client.HttpClient = guardedSDKHTTP{ctx: ctx, origin: "fcsandbox.ap-southeast-1.aliyuncs.com"}
		return &client
	}
	return m.API
}
func sdkOptions() *dara.RuntimeOptions {
	return &dara.RuntimeOptions{Autoretry: dara.Bool(false), MaxAttempts: dara.Int(1), ReadTimeout: dara.Int(25000), ConnectTimeout: dara.Int(5000), IgnoreSSL: dara.Bool(false)}
}
func (m Management) CreateKey(ctx context.Context, name string, expires time.Time) (ControlKey, error) {
	api := m.api(ctx)
	if api == nil || m.TeamID == "" || !transportID.MatchString(name) || !expires.After(time.Now()) || expires.After(time.Now().Add(900*time.Second)) || ctx.Err() != nil {
		return ControlKey{}, ErrConfiguration
	}
	res, e := api.CreateApiKeyWithOptions(&fc.CreateApiKeyRequest{Body: &fc.CreateApiKeyInput{ApiKeyName: dara.String(name), TeamID: dara.String(m.TeamID), ExpireTime: dara.String(expires.UTC().Format(time.RFC3339Nano))}}, nil, sdkOptions())
	if e != nil || res == nil || res.Body == nil || res.Body.ApiKey == nil {
		return ControlKey{}, ErrKeyCreateUnconfirmed
	}
	k := res.Body.ApiKey
	if dara.StringValue(k.ApiKeyID) == "" || dara.StringValue(k.ApiKeyValue) == "" || dara.StringValue(k.TeamID) != m.TeamID {
		return ControlKey{}, ErrKeyCreateUnconfirmed
	}
	return ControlKey{ID: dara.StringValue(k.ApiKeyID), value: dara.StringValue(k.ApiKeyValue)}, nil
}
func (m Management) InventoryKeys(ctx context.Context) ([]KeyInfo, error) {
	api := m.api(ctx)
	if api == nil || m.TeamID == "" || m.AccountID == "" {
		return nil, ErrConfiguration
	}
	out := []KeyInfo{}
	ids := map[string]bool{}
	var total int32 = -1
	for page := int32(1); page <= 100; page++ {
		if ctx.Err() != nil {
			return nil, ErrKeyInventoryUnavailable
		}
		res, e := api.ListApiKeysWithOptions(&fc.ListApiKeysRequest{TeamID: dara.String(m.TeamID), PageNumber: dara.Int32(page), PageSize: dara.Int32(100)}, nil, sdkOptions())
		if e != nil || res == nil || res.Body == nil || res.Body.Total == nil {
			return nil, ErrKeyInventoryUnavailable
		}
		b := res.Body
		if total < 0 {
			total = dara.Int32Value(b.Total)
		}
		if total < 0 || total > 10000 || dara.Int32Value(b.Total) != total || len(b.ApiKeys) > 100 {
			return nil, ErrKeyInventoryUnavailable
		}
		for _, k := range b.ApiKeys {
			if k == nil {
				return nil, ErrKeyInventoryUnavailable
			}
			v := KeyInfo{ID: dara.StringValue(k.ApiKeyID), Name: dara.StringValue(k.ApiKeyName), TeamID: dara.StringValue(k.TeamID), AccountID: dara.StringValue(k.UserID)}
			if v.ID == "" || v.TeamID != m.TeamID || v.AccountID != m.AccountID || ids[v.ID] {
				return nil, ErrKeyInventoryUnavailable
			}
			ids[v.ID] = true
			out = append(out, v)
		}
		if int32(len(out)) == total {
			return out, nil
		}
		if len(b.ApiKeys) == 0 || int32(len(out)) > total {
			return nil, ErrKeyInventoryUnavailable
		}
	}
	return nil, ErrKeyInventoryUnavailable
}
func (m Management) RevokeKey(ctx context.Context, id string) error {
	keys, e := m.InventoryKeys(ctx)
	if e != nil {
		return ErrKeyCleanupUnconfirmed
	}
	found := false
	for _, k := range keys {
		found = found || k.ID == id
	}
	if !found {
		return nil
	}
	api := m.api(ctx)
	_, e = api.UpdateApiKeyWithOptions(dara.String(id), &fc.UpdateApiKeyRequest{Body: &fc.UpdateApiKeyInput{Status: dara.String("inactive")}}, nil, sdkOptions())
	if e != nil {
		return ErrKeyCleanupUnconfirmed
	}
	if _, e = api.DeleteApiKeyWithOptions(dara.String(id), &fc.DeleteApiKeyRequest{}, nil, sdkOptions()); e != nil {
		return ErrKeyCleanupUnconfirmed
	}
	keys, e = m.InventoryKeys(ctx)
	if e != nil {
		return ErrKeyCleanupUnconfirmed
	}
	for _, k := range keys {
		if k.ID == id {
			return ErrKeyCleanupUnconfirmed
		}
	}
	return nil
}
func (k ControlKey) GoString() string { return fmt.Sprintf("[control key %s]", k.ID) }
