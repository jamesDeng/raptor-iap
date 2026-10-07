package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	fc "github.com/alibabacloud-go/fcsandbox-20260509/client"
	"github.com/alibabacloud-go/tea/dara"
	"testing"
	"time"
)

type keyAPI struct {
	pages      int
	removed    bool
	failDelete bool
}

func (a *keyAPI) CreateApiKeyWithOptions(r *fc.CreateApiKeyRequest, h map[string]*string, o *dara.RuntimeOptions) (*fc.CreateApiKeyResponse, error) {
	return &fc.CreateApiKeyResponse{Body: &fc.CreateApiKeyResponseBody{ApiKey: &fc.ApiKey{ApiKeyID: dara.String("key-id"), ApiKeyValue: dara.String("never-persist-value"), TeamID: r.Body.TeamID, ApiKeyName: r.Body.ApiKeyName}}}, nil
}
func (a *keyAPI) ListApiKeysWithOptions(r *fc.ListApiKeysRequest, h map[string]*string, o *dara.RuntimeOptions) (*fc.ListApiKeysResponse, error) {
	a.pages++
	keys := []*fc.ApiKey{}
	if !a.removed && dara.Int32Value(r.PageNumber) == 1 {
		keys = append(keys, &fc.ApiKey{ApiKeyID: dara.String("key-id"), ApiKeyName: dara.String("attempt-key"), TeamID: dara.String("team"), UserID: dara.String("account")})
	}
	return &fc.ListApiKeysResponse{Body: &fc.ListApiKeysResponseBody{ApiKeys: keys, Total: dara.Int32(int32(len(keys))), PageNumber: r.PageNumber}}, nil
}
func (a *keyAPI) UpdateApiKeyWithOptions(id *string, r *fc.UpdateApiKeyRequest, h map[string]*string, o *dara.RuntimeOptions) (*fc.UpdateApiKeyResponse, error) {
	if dara.StringValue(r.Body.Status) != "inactive" {
		panic("wrong deactivation")
	}
	return &fc.UpdateApiKeyResponse{}, nil
}
func (a *keyAPI) DeleteApiKeyWithOptions(id *string, r *fc.DeleteApiKeyRequest, h map[string]*string, o *dara.RuntimeOptions) (*fc.DeleteApiKeyResponse, error) {
	if a.failDelete {
		return nil, fmt.Errorf("private provider failure")
	}
	a.removed = true
	return &fc.DeleteApiKeyResponse{}, nil
}
func TestControlKeysNeverSerializeAndRemovalIsConfirmed(t *testing.T) {
	a := &keyAPI{}
	m := Management{API: a, TeamID: "team", AccountID: "account"}
	key, e := m.CreateKey(context.Background(), "attempt-key", time.Now().Add(time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(key)
	if string(raw) != "{\"id\":\"key-id\"}" || fmt.Sprint(key) != "[control key key-id]" {
		t.Fatal("secret serializable")
	}
	if e = m.RevokeKey(context.Background(), key.ID); e != nil || !a.removed || a.pages < 2 {
		t.Fatal("cleanup not confirmed", e)
	}
}
func TestControlDeletionFailureDoesNotClaimAbsence(t *testing.T) {
	m := Management{API: &keyAPI{failDelete: true}, TeamID: "team", AccountID: "account"}
	if e := m.RevokeKey(context.Background(), "key-id"); e != ErrKeyCleanupUnconfirmed {
		t.Fatal(e)
	}
}
func TestManagementLeaseCheckedBeforeEachMutation(t *testing.T) {
	a := &keyAPI{}
	m := Management{API: a, TeamID: "team", AccountID: "account", before: func(context.Context) error {
		if a.pages > 0 {
			return ErrRuntimeUnavailable
		}
		return nil
	}}
	if e := m.RevokeKey(context.Background(), "key-id"); e == nil || a.removed {
		t.Fatal("continued after ownership loss", e)
	}
}
