package modelproviders

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
)

type HTTPService struct {
	Pool        *pgxpool.Pool
	Policy      PolicyStore
	Credentials *CredentialStore
	Connect     *Service
	Flow        NodeFlow
}

func sessionOwner(r *http.Request) string {
	c, _ := r.Cookie("raptor_session")
	if c == nil {
		return ""
	}
	h := sha256.Sum256([]byte(c.Value))
	return auth.UserFrom(r).ID + ":" + hex.EncodeToString(h[:])
}

func (s *HTTPService) Register(m *http.ServeMux, a *auth.Service) {
	m.Handle("GET /api/v1/model-providers", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		models, err := s.Policy.ListEnabled(r.Context())
		if err != nil {
			httpx.Error(w, 503, "Unavailable")
			return
		}
		httpx.Write(w, 200, map[string]any{"models": models})
	})))
	m.Handle("GET /api/v1/admin/model-providers", a.Admin(http.HandlerFunc(s.adminList)))
	m.Handle("POST /api/v1/admin/model-providers/codex/connect", a.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		challenge, err := s.Connect.StartCodexConnect(r.Context(), sessionOwner(r))
		if err != nil {
			httpx.Error(w, 503, "ProviderAuthorizationUnavailable")
			return
		}
		httpx.Write(w, 202, challenge)
	})))
	m.Handle("GET /api/v1/admin/model-providers/codex/connect/{sessionId}", a.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state, err := s.Connect.PollCodexConnect(r.Context(), sessionOwner(r), r.PathValue("sessionId"))
		if errors.Is(err, ErrForbidden) {
			httpx.Error(w, 403, "Forbidden")
			return
		}
		if err != nil {
			httpx.Error(w, 404, "NotFound")
			return
		}
		httpx.Write(w, 200, state)
	})))
	m.Handle("DELETE /api/v1/admin/model-providers/codex/connect/{sessionId}", a.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.Connect.CancelCodexConnect(r.Context(), sessionOwner(r), r.PathValue("sessionId")); err != nil {
			httpx.Error(w, 409, "CannotCancel")
			return
		}
		httpx.Write(w, 200, map[string]bool{"cancelled": true})
	})))
	m.Handle("POST /api/v1/admin/model-providers/codex/discover", a.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		models, err := s.Flow.Discover(r.Context())
		if err == nil {
			err = s.Policy.ReplaceCatalog(r.Context(), "codex", models, auth.UserFrom(r).ID)
		}
		if err != nil {
			httpx.Error(w, 503, "ModelDiscoveryFailed")
			return
		}
		httpx.Write(w, 200, map[string]any{"models": models})
	})))
	m.Handle("PUT /api/v1/admin/model-providers/{providerId}/models", a.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ExpectedVersion int64     `json:"expectedVersion"`
			Enabled         []ModelID `json:"enabled"`
			DefaultID       ModelID   `json:"defaultModelId"`
		}
		if !httpx.Decode(w, r, &in) {
			return
		}
		err := s.Policy.SetPolicy(r.Context(), auth.UserFrom(r).ID, ProviderID(r.PathValue("providerId")), in.ExpectedVersion, in.Enabled, in.DefaultID)
		if errors.Is(err, ErrPolicyConflict) {
			httpx.Error(w, 409, "PolicyConflict")
			return
		}
		if errors.Is(err, ErrInvalidModel) {
			httpx.Error(w, 400, "InvalidModelPolicy")
			return
		}
		if err != nil {
			httpx.Error(w, 503, "Unavailable")
			return
		}
		httpx.Write(w, 200, map[string]bool{"updated": true})
	})))
	m.Handle("POST /api/v1/admin/model-providers/codex/disconnect", a.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Connect.CancelPending()
		err := s.Credentials.Disconnect(r.Context(), "codex", auth.UserFrom(r).ID)
		if err != nil {
			httpx.Error(w, 503, "Unavailable")
			return
		}
		httpx.Write(w, 200, map[string]bool{"disconnected": true})
	})))
}

func (s *HTTPService) RegisterPrivate(m *http.ServeMux, credentials auth.Credentials) {
	m.Handle("POST /internal/v1/model-credentials/validate", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var binding AttemptBinding
		if !httpx.Decode(w, r, &binding) {
			return
		}
		if s.ValidateAttempt(r.Context(), binding) != nil {
			httpx.Error(w, 403, "ModelAttemptRevoked")
			return
		}
		httpx.Write(w, 200, map[string]bool{"valid": true})
	}), credentials))
	m.Handle("POST /internal/v1/model-credentials/lease", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var binding AttemptBinding
		if !httpx.Decode(w, r, &binding) {
			return
		}
		lease, err := s.LeaseCredential(r.Context(), binding)
		if err != nil {
			httpx.Error(w, 403, "ModelCredentialDenied")
			return
		}
		httpx.Write(w, 200, lease)
	}), credentials))
	m.Handle("POST /internal/v1/model-credentials/refresh", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Binding            AttemptBinding  `json:"binding"`
			ExpectedGeneration int64           `json:"expectedGeneration"`
			Credential         json.RawMessage `json:"credential"`
		}
		if !httpx.Decode(w, r, &in) {
			return
		}
		generation, err := s.RefreshCredential(r.Context(), in.Binding, in.ExpectedGeneration, in.Credential)
		if err != nil {
			httpx.Error(w, 403, "ModelCredentialRefreshDenied")
			return
		}
		httpx.Write(w, 200, map[string]int64{"generation": generation})
	}), credentials))
}

func (s *HTTPService) adminList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Pool.Query(r.Context(), `SELECT c.provider_id,c.status,c.account_label,c.connection_version,COALESCE(p.version,0),COALESCE(p.enabled_ids,'[]'::jsonb),COALESCE(p.default_model_id,'') FROM raptor.model_provider_connections c LEFT JOIN raptor.model_provider_policy p USING(provider_id) ORDER BY c.provider_id`)
	if err != nil {
		httpx.Error(w, 503, "Unavailable")
		return
	}
	defer rows.Close()
	type connection struct {
		ProviderID        string    `json:"providerId"`
		Status            string    `json:"status"`
		AccountLabel      string    `json:"accountLabel"`
		ConnectionVersion int64     `json:"connectionVersion"`
		PolicyVersion     int64     `json:"policyVersion"`
		Enabled           []ModelID `json:"enabled"`
		DefaultModelID    string    `json:"defaultModelId"`
		Models            []struct {
			ModelID     string `json:"modelId"`
			DisplayName string `json:"displayName"`
			Available   bool   `json:"available"`
		} `json:"models"`
	}
	out := []connection{}
	for rows.Next() {
		var c connection
		var enabled []byte
		if rows.Scan(&c.ProviderID, &c.Status, &c.AccountLabel, &c.ConnectionVersion, &c.PolicyVersion, &enabled, &c.DefaultModelID) != nil {
			httpx.Error(w, 503, "Unavailable")
			return
		}
		if json.Unmarshal(enabled, &c.Enabled) != nil {
			httpx.Error(w, 503, "Unavailable")
			return
		}
		out = append(out, c)
	}
	if rows.Err() != nil {
		httpx.Error(w, 503, "Unavailable")
		return
	}
	rows.Close()
	for i := range out {
		modelRows, e := s.Pool.Query(r.Context(), `SELECT model_id,display_name,available FROM raptor.model_provider_models WHERE provider_id=$1 ORDER BY model_id`, out[i].ProviderID)
		if e != nil {
			httpx.Error(w, 503, "Unavailable")
			return
		}
		for modelRows.Next() {
			var model struct {
				ModelID     string `json:"modelId"`
				DisplayName string `json:"displayName"`
				Available   bool   `json:"available"`
			}
			if modelRows.Scan(&model.ModelID, &model.DisplayName, &model.Available) != nil {
				modelRows.Close()
				httpx.Error(w, 503, "Unavailable")
				return
			}
			out[i].Models = append(out[i].Models, model)
		}
		if modelRows.Err() != nil {
			modelRows.Close()
			httpx.Error(w, 503, "Unavailable")
			return
		}
		modelRows.Close()
	}
	if len(out) == 0 {
		out = append(out, connection{ProviderID: "codex", Status: "disconnected", Enabled: []ModelID{}})
	}
	httpx.Write(w, 200, map[string]any{"providers": out})
}
