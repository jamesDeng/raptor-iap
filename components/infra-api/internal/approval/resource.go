package approval

import (
	"context"
	"errors"
	"net/http"
	"raptor-iap/infra-api/internal/resources"
	"time"
)

// NewResource keeps proof resolution in the calling Infra invocation. The
// private prepare response cannot authorize an operation without final validation.
func NewResource(origin, user, password string, client *http.Client, resolve func(context.Context, string, string) (resources.Proof, error)) (*Client, error) {
	if resolve == nil {
		return nil, errors.New("resource proof resolver required")
	}
	c, e := New(origin, user, password, client)
	if e != nil {
		return nil, e
	}
	c.resolveProof = resolve
	return c, nil
}
func (c *Client) resourceCall(ctx context.Context, b Binding, operation string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var prepared struct {
		Data struct {
			Eligible bool   `json:"eligible"`
			Prepared string `json:"prepared"`
		} `json:"data"`
	}
	input := struct {
		Operation string `json:"operation"`
		Binding   any    `json:"binding"`
	}{operation, b.Wire()}
	if e := c.submissionCall(ctx, "/v1/infra/approval-prepare", input, &prepared); e != nil {
		return e
	}
	if !prepared.Data.Eligible || len(prepared.Data.Prepared) != 64 {
		return errors.New("approval unavailable")
	}
	proof, e := c.resolveProof(ctx, b.EnvCode, b.ProxyCode)
	if e != nil {
		return e
	}
	digest, e := resources.Digest(proof.Identity)
	if e != nil || digest != proof.ResolutionDigest || proof.Identity.EnvCode != b.EnvCode || proof.Identity.ProxyCode != b.ProxyCode || proof.Identity.GroupID != b.GroupID {
		return errors.New("resource identity changed")
	}
	final := struct {
		Operation string          `json:"operation"`
		Binding   any             `json:"binding"`
		Prepared  string          `json:"prepared"`
		Proof     resources.Proof `json:"proof"`
	}{operation, b.Wire(), prepared.Data.Prepared, proof}
	return c.submissionCall(ctx, "/v1/infra/approval-validate", final, out)
}
