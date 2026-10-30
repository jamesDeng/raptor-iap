package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type limitedOutput struct {
	data  []byte
	limit int
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > b.limit {
		return 0, ErrRuntimeUnavailable
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func gatewayObservationSecrets(credential string) []string {
	if credential == "" {
		return nil
	}
	plain := "agent:" + credential
	encoded := base64.StdEncoding.EncodeToString([]byte(plain))
	return []string{credential, plain, encoded, "Basic " + encoded}
}

// Private execution input is built from the existing trusted attempt access.
func gatewayObservationInput(ctx context.Context, in execution.LiveStart, m ObservationMaterial, tool string, args json.RawMessage) ([]byte, time.Time, error) {
	bad := func() ([]byte, time.Time, error) { return nil, time.Time{}, ErrConfiguration }
	if ctx.Err() != nil || !validObservationInput(tool, args) || in.Access.Binding != in.Binding || in.Access.ReplacementScope == nil || !in.Access.ReplacementScope.ValidFor(in.Binding) || !regexp.MustCompile(`^raptor_at_[A-Za-z0-9_-]{43}$`).MatchString(in.Access.Credential) {
		return bad()
	}
	endpoint, err := url.Parse(in.Access.InfraMcpURL)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || !strings.HasSuffix(endpoint.Path, "/mcp") {
		return bad()
	}
	now := time.Now()
	deadline := now.Add(20 * time.Second)
	for _, until := range []time.Time{in.Deadline, in.Access.ExpiresAt} {
		if !until.After(now) {
			return bad()
		}
		if until.Before(deadline) {
			deadline = until
		}
	}
	if until, ok := ctx.Deadline(); ok && until.Before(deadline) {
		deadline = until
	}
	var config, kube any
	if json.Unmarshal(m.Config, &config) != nil || json.Unmarshal(m.Kubeconfig, &kube) != nil {
		return bad()
	}
	input, err := json.Marshal(map[string]any{"binding": in.Binding, "scope": in.Access.ReplacementScope, "config": config, "kubeconfig": kube, "tool": tool, "arguments": args, "infra": map[string]string{"url": in.Access.InfraMcpURL, "credential": in.Access.Credential}, "deadlineAt": deadline.UTC()})
	if err != nil || len(input) > 131072 {
		return bad()
	}
	return input, deadline, nil
}

// Gateway executes its packaged validator, never an agent-supplied program.
func ExecuteGatewayObservation(ctx context.Context, in execution.LiveStart, m ObservationMaterial, tool string, args json.RawMessage) (json.RawMessage, error) {
	input, deadline, err := gatewayObservationInput(ctx, in, m, tool, args)
	if err != nil {
		return nil, err
	}
	call, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	cmd := exec.CommandContext(call, "/usr/bin/node", "/opt/raptor-harness/gateway-observation-runner.mjs")
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	cmd.Stdin = bytes.NewReader(input)
	out := &limitedOutput{limit: 262144}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if cmd.Run() != nil || call.Err() != nil || !time.Now().Before(deadline) || !json.Valid(out.data) {
		return nil, ErrRuntimeUnavailable
	}
	for _, secret := range gatewayObservationSecrets(in.Access.Credential) {
		if bytes.Contains(out.data, []byte(secret)) {
			return nil, ErrRuntimeUnavailable
		}
	}
	return json.RawMessage(out.data), nil
}
