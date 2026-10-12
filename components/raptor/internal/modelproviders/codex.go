package modelproviders

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type NodeFlow struct{ NodePath, HelperPath string }

func (f NodeFlow) Discover(ctx context.Context) ([]DiscoveredModel, error) {
	if !filepath.IsAbs(f.NodePath) || !filepath.IsAbs(f.HelperPath) {
		return nil, errors.New("invalid model helper path")
	}
	cmd := exec.CommandContext(ctx, f.NodePath, f.HelperPath, "--models")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "PI_OFFLINE=1", "PI_TELEMETRY=0"}
	cmd.Stderr = io.Discard
	output, err := cmd.Output()
	if err != nil || len(output) > 65536 {
		return nil, errors.New("model discovery failed")
	}
	var result struct {
		Models []DiscoveredModel `json:"models"`
	}
	if json.Unmarshal(output, &result) != nil || len(result.Models) == 0 {
		return nil, errors.New("model discovery failed")
	}
	for _, model := range result.Models {
		if model.ModelID == "" || model.DisplayName == "" {
			return nil, errors.New("invalid model catalog")
		}
	}
	return result.Models, nil
}

func NewNodeFlow(nodePath, helperPath string) NodeFlow {
	return NodeFlow{NodePath: nodePath, HelperPath: helperPath}
}

type nodeAttempt struct {
	command   *exec.Cmd
	scanner   *bufio.Scanner
	challenge ConnectChallenge
	stdin     io.WriteCloser
	state     string
	submitted bool
}

func (a *nodeAttempt) Challenge() ConnectChallenge { return a.challenge }

func (f NodeFlow) Start(ctx context.Context) (DeviceAttempt, error) {
	return f.start(ctx, false)
}

func (f NodeFlow) StartBrowser(ctx context.Context) (BrowserAttempt, error) {
	return f.start(ctx, true)
}

func (f NodeFlow) start(ctx context.Context, browser bool) (*nodeAttempt, error) {
	if !filepath.IsAbs(f.NodePath) || !filepath.IsAbs(f.HelperPath) {
		return nil, errors.New("invalid auth helper path")
	}
	args := []string{f.HelperPath}
	if browser {
		args = append(args, "--browser")
	}
	cmd := exec.Command(f.NodePath, args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/nonexistent", "PI_OFFLINE=1", "PI_TELEMETRY=0"}
	cmd.Stderr = io.Discard
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	var stdin io.WriteCloser
	if browser {
		stdin, e = cmd.StdinPipe()
		if e != nil {
			return nil, e
		}
	}
	if e = cmd.Start(); e != nil {
		return nil, e
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 65536)
	type first struct {
		raw []byte
		err error
	}
	ready := make(chan first, 1)
	go func() {
		if scanner.Scan() {
			ready <- first{raw: append([]byte(nil), scanner.Bytes()...)}
			return
		}
		ready <- first{err: errors.New("auth helper ended without challenge")}
	}()
	var line first
	select {
	case line = <-ready:
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, ctx.Err()
	}
	if line.err != nil {
		_ = cmd.Wait()
		return nil, line.err
	}
	var wire struct{ Type, VerificationURL, AuthorizationURL, UserCode, ExpiresAt string }
	if json.Unmarshal(line.raw, &wire) != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, errors.New("invalid auth challenge")
	}
	var state string
	if browser {
		u, e := url.Parse(wire.AuthorizationURL)
		if e != nil || wire.Type != "browser_challenge" || u.Scheme != "https" || u.Host != "auth.openai.com" || u.Path != "/oauth/authorize" || u.Query().Get("redirect_uri") != "http://localhost:1455/auth/callback" || u.Query().Get("client_id") == "" || u.Query().Get("code_challenge") == "" || u.Query().Get("state") == "" {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, errors.New("invalid browser challenge")
		}
		state = u.Query().Get("state")
	} else if wire.Type != "challenge" || wire.VerificationURL != "https://auth.openai.com/codex/device" || wire.UserCode == "" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, errors.New("invalid auth challenge")
	}
	expires, e := time.Parse(time.RFC3339Nano, wire.ExpiresAt)
	max := 16 * time.Minute
	if browser {
		max = 16 * time.Minute
	}
	if e != nil || !expires.After(time.Now()) || expires.After(time.Now().Add(max)) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, errors.New("invalid auth challenge expiry")
	}
	return &nodeAttempt{command: cmd, scanner: scanner, stdin: stdin, state: state, challenge: ConnectChallenge{VerificationURL: wire.VerificationURL, AuthorizationURL: wire.AuthorizationURL, UserCode: wire.UserCode, ExpiresAt: expires}}, nil
}

func (a *nodeAttempt) SubmitRedirect(raw string) error {
	if a.stdin == nil || a.submitted || len(raw) > 4096 {
		return ErrClosed
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "http" || u.Host != "localhost:1455" || u.Path != "/auth/callback" || u.User != nil || u.Fragment != "" || u.Query().Get("state") != a.state || u.Query().Get("code") == "" {
		return ErrInvalidRedirect
	}
	a.submitted = true
	_, e = io.WriteString(a.stdin, raw+"\n")
	_ = a.stdin.Close()
	return e
}

func (a *nodeAttempt) Await(ctx context.Context) ([]byte, error) {
	type result struct {
		raw []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		if !a.scanner.Scan() {
			done <- result{err: errors.New("auth helper ended")}
			return
		}
		done <- result{raw: append([]byte(nil), a.scanner.Bytes()...)}
	}()
	var line result
	select {
	case line = <-done:
	case <-ctx.Done():
		_ = a.command.Process.Kill()
		_ = a.command.Wait()
		return nil, ctx.Err()
	}
	_ = a.command.Process.Kill()
	_ = a.command.Wait()
	if line.err != nil {
		return nil, line.err
	}
	var wire struct {
		Type       string          `json:"type"`
		Credential json.RawMessage `json:"credential"`
	}
	if json.Unmarshal(line.raw, &wire) != nil || wire.Type != "credential" || len(wire.Credential) == 0 {
		return nil, errors.New("auth helper failed")
	}
	var credential struct {
		Type    string `json:"type"`
		Access  string `json:"access"`
		Refresh string `json:"refresh"`
		Expires int64  `json:"expires"`
	}
	if json.Unmarshal(wire.Credential, &credential) != nil || credential.Type != "oauth" || credential.Access == "" || credential.Refresh == "" || credential.Expires <= 0 {
		return nil, errors.New("invalid auth credential")
	}
	return wire.Credential, nil
}
