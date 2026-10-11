package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"os/exec"
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

// Gateway executes its packaged validator, never an agent-supplied program.
func ExecuteGatewayObservation(ctx context.Context, in execution.LiveStart, m ObservationMaterial, tool string, args json.RawMessage) (json.RawMessage, error) {
	if !validObservationInput(tool, args) || in.Access.ReplacementScope == nil {
		return nil, ErrConfiguration
	}
	var config any
	if json.Unmarshal(m.Config, &config) != nil {
		return nil, ErrConfiguration
	}
	var kube any
	if json.Unmarshal(m.Kubeconfig, &kube) != nil {
		return nil, ErrConfiguration
	}
	input, e := json.Marshal(map[string]any{"binding": in.Binding, "scope": in.Access.ReplacementScope, "config": config, "kubeconfig": kube, "tool": tool, "arguments": args})
	if e != nil || len(input) > 131072 {
		return nil, ErrConfiguration
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/node", "/opt/raptor-harness/gateway-observation-runner.mjs")
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	cmd.Stdin = bytes.NewReader(input)
	out := &limitedOutput{limit: 262144}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if cmd.Run() != nil || !json.Valid(out.data) {
		return nil, ErrRuntimeUnavailable
	}
	return json.RawMessage(out.data), nil
}
