package approval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"raptor-iap/infra-api/internal/jsoninput"
	"time"
)

func validBinding(b Binding) bool {
	return b.RequestID != "" && b.ActionID != "" && b.EnvCode != "" && b.ProxyCode != "" && b.GroupID != "" && b.DesiredCapacity >= 0
}
func (c *Client) submissionCall(ctx context.Context, path string, in any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b, e := json.Marshal(in)
	if e != nil || len(b) > 8192 {
		return errors.New("invalid submission binding")
	}
	r, e := http.NewRequestWithContext(ctx, "POST", c.origin+path, bytes.NewReader(b))
	if e != nil {
		return errors.New("submission unavailable")
	}
	r.SetBasicAuth(c.user, c.password)
	r.Header.Set("Content-Type", "application/json")
	response, e := c.http.Do(r)
	if e != nil {
		return errors.New("submission unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("submission unavailable")
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 8193))
	if e != nil || len(raw) > 8192 || jsoninput.Decode(raw, out) != nil {
		return errors.New("submission unavailable")
	}
	return nil
}
func (c *Client) Claim(ctx context.Context, b Binding) (bool, error) {
	if !validBinding(b) {
		return false, errors.New("invalid submission binding")
	}
	var out struct {
		Data struct {
			Claimed *bool `json:"claimed"`
		} `json:"data"`
	}
	if e := c.submissionCall(ctx, "/v1/approval-claim", b.Wire(), &out); e != nil {
		return false, e
	}
	if out.Data.Claimed == nil {
		return false, errors.New("submission unavailable")
	}
	return *out.Data.Claimed, nil
}
func (c *Client) Record(ctx context.Context, b Binding, outcome, providerID string) error {
	if !validBinding(b) || len(providerID) > 512 {
		return errors.New("invalid submission binding")
	}
	switch outcome {
	case "submitted":
		if providerID == "" {
			return errors.New("invalid submission receipt")
		}
	case "unknown":
	case "not_submitted":
		if providerID != "" {
			return errors.New("invalid submission receipt")
		}
	default:
		return errors.New("invalid submission receipt")
	}
	in := struct {
		Binding           any    `json:"binding"`
		Outcome           string `json:"outcome"`
		ProviderRequestID string `json:"providerRequestId"`
	}{b.Wire(), outcome, providerID}
	var out struct {
		Data struct {
			Recorded *bool `json:"recorded"`
		} `json:"data"`
	}
	if e := c.submissionCall(ctx, "/v1/approval-record", in, &out); e != nil {
		return e
	}
	if out.Data.Recorded == nil || !*out.Data.Recorded {
		return errors.New("submission unavailable")
	}
	return nil
}
