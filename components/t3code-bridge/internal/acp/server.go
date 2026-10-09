// Package acp exposes only remote Raptor application questions over ACP v1.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/bridge"
	"io"
	"path/filepath"
	"strings"
	"sync"
)

type Agent interface {
	NewSession(context.Context) (string, error)
	LoadSession(context.Context, string, func(bridge.Update) error) error
	Prompt(context.Context, string, string, func(bridge.Update) error) (bridge.Outcome, error)
	Cancel(context.Context, string) error
}
type frame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type server struct {
	agent       Agent
	out         io.Writer
	mu          sync.Mutex
	writeErr    error
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	initialized bool
}

func (s *server) write(v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	b, e := json.Marshal(v)
	if e == nil {
		_, e = s.out.Write(append(b, '\n'))
	}
	if e != nil {
		s.writeErr = e
		s.cancel()
	}
	return e
}
func (s *server) reply(id json.RawMessage, result any, e error) {
	if len(id) == 0 {
		return
	}
	m := map[string]any{"jsonrpc": "2.0", "id": id}
	if e != nil {
		code := -32000
		if errors.Is(e, errParams) {
			code = -32602
		}
		if errors.Is(e, errMethod) {
			code = -32601
		}
		m["error"] = map[string]any{"code": code, "message": e.Error()}
	} else {
		m["result"] = result
	}
	s.write(m)
}

var errParams = errors.New("InvalidParams")
var errMethod = errors.New("MethodNotFound")

func (s *server) emitter(id string) func(bridge.Update) error {
	seen := map[string]bool{}
	return func(u bridge.Update) error {
		update := map[string]any{}
		switch u.Kind {
		case "text", "user":
			kind := "agent_message_chunk"
			if u.Kind == "user" {
				kind = "user_message_chunk"
			}
			update = map[string]any{"sessionUpdate": kind, "content": map[string]string{"type": "text", "text": u.Text}}
		case "progress":
			kind := "tool_call_update"
			if !seen[u.ID] {
				kind = "tool_call"
				seen[u.ID] = true
			}
			update = map[string]any{"sessionUpdate": kind, "toolCallId": u.ID, "title": u.Text, "status": u.Status, "kind": "other"}
		default:
			return errParams
		}
		return s.write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": id, "update": update}})
	}
}
func Serve(parent context.Context, input io.Reader, output io.Writer, agent Agent) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	// Closing owned stdin interrupts Scan on signals and broken output.
	stopClose := context.AfterFunc(ctx, func() {
		if c, ok := input.(io.ReadCloser); ok {
			_ = c.Close()
		}
	})
	defer stopClose()
	s := &server{agent: agent, out: output, cancel: cancel}
	scan := bufio.NewScanner(input)
	scan.Buffer(make([]byte, 4096), 1<<20)

	frames := make(chan []byte)
	scanDone := make(chan error, 1)
	go func() {
		for scan.Scan() {
			b := append([]byte(nil), scan.Bytes()...)
			select {
			case frames <- b:
			case <-ctx.Done():
				return
			}
		}
		scanDone <- scan.Err()
	}()
	var scanErr error
reading:
	for {
		var line []byte
		select {
		case <-ctx.Done():
			break reading
		case scanErr = <-scanDone:
			break reading
		case line = <-frames:
		}

		var f frame
		if json.Unmarshal(line, &f) != nil || f.JSONRPC != "2.0" || f.Method == "" {
			s.reply(json.RawMessage(`null`), nil, errParams)
			continue
		}
		if len(f.ID) > 0 && string(f.ID) != "null" {
			var id any
			if json.Unmarshal(f.ID, &id) != nil {
				s.reply(json.RawMessage(`null`), nil, errParams)
				continue
			}
			switch id.(type) {
			case string, float64:
			default:
				s.reply(json.RawMessage(`null`), nil, errParams)
				continue
			}
		}
		if ctx.Err() != nil {
			break
		}
		if f.Method == "initialize" {
			var p struct {
				Version int `json:"protocolVersion"`
			}
			if json.Unmarshal(f.Params, &p) != nil || p.Version < 1 {
				s.reply(f.ID, nil, errParams)
				continue
			}
			s.initialized = true
			s.reply(f.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"loadSession": true, "promptCapabilities": map[string]bool{"image": false, "audio": false, "embeddedContext": false}, "mcpCapabilities": map[string]bool{"http": false, "sse": false}}, "agentInfo": map[string]string{"name": "raptor-acp", "title": "Raptor Infra Ops", "version": "0.1.0"}, "authMethods": []any{}}, nil)
			continue
		}
		if !s.initialized {
			s.reply(f.ID, nil, errors.New("InitializeRequired"))
			continue
		}
		var p struct {
			SessionID string `json:"sessionId"`
			CWD       string `json:"cwd"`
			Prompt    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"prompt"`
		}
		if json.Unmarshal(f.Params, &p) != nil {
			s.reply(f.ID, nil, errParams)
			continue
		}
		if f.Method == "session/cancel" {
			if p.SessionID != "" {
				s.wg.Add(1)
				go func() { defer s.wg.Done(); e := agent.Cancel(ctx, p.SessionID); s.reply(f.ID, map[string]any{}, e) }()
			}
			continue
		}
		if len(f.ID) == 0 {
			continue
		}
		switch f.Method {
		case "session/new":
			if !filepath.IsAbs(p.CWD) {
				s.reply(f.ID, nil, errParams)
				continue
			}
			id, e := agent.NewSession(ctx)
			s.reply(f.ID, map[string]any{"sessionId": id, "models": map[string]any{"currentModelId": "gpt-5.6-luna", "availableModels": []any{map[string]string{"modelId": "gpt-5.6-luna", "name": "GPT-5.6 Luna"}}}}, e)
		case "session/load":
			if p.SessionID == "" || !filepath.IsAbs(p.CWD) {
				s.reply(f.ID, nil, errParams)
				continue
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				e := agent.LoadSession(ctx, p.SessionID, s.emitter(p.SessionID))
				s.reply(f.ID, map[string]any{}, e)
			}()
		case "session/prompt":
			if p.SessionID == "" || len(p.Prompt) == 0 {
				s.reply(f.ID, nil, errParams)
				continue
			}
			parts := []string{}
			valid := true
			for _, c := range p.Prompt {
				if c.Type != "text" {
					valid = false
				}
				parts = append(parts, c.Text)
			}
			if !valid {
				s.reply(f.ID, nil, errParams)
				continue
			}
			text, parseErr := questionText(parts)
			if parseErr != nil {
				s.reply(f.ID, nil, parseErr)
				continue
			}
			ready := make(chan struct{})
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()

				var o bridge.Outcome
				var e error
				if a, ok := agent.(interface {
					PromptReady(context.Context, string, string, func(bridge.Update) error, func()) (bridge.Outcome, error)
				}); ok {
					o, e = a.PromptReady(ctx, p.SessionID, text, s.emitter(p.SessionID), func() { close(ready) })
				} else {
					close(ready)
					o, e = agent.Prompt(ctx, p.SessionID, text, s.emitter(p.SessionID))
				}
				stop := "end_turn"
				if o.Status == "cancelled" {
					stop = "cancelled"
				}
				s.reply(f.ID, map[string]string{"stopReason": stop}, e)
			}()
			select {
			case <-ready:
			case <-ctx.Done():
			}
		default:
			s.reply(f.ID, nil, errMethod)
		}
	}
	cancel()
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	if scanErr != nil && parent.Err() == nil {
		return errors.New("InvalidACPFrame")
	}
	return nil
}

// The pinned T3 release wraps user text separately from its coding/runtime scaffolding.
// Require exact, unambiguous boundaries rather than truncating an oversized prompt.
func questionText(parts []string) (string, error) {
	if len(parts) == 0 {
		return "", errParams
	}
	first := parts[0]
	if !strings.HasPrefix(first, "<t3_code_instructions>\n") {
		return strings.Join(parts, "\n"), nil
	}
	const boundary = "</t3_code_instructions>\n\n<user_request>\n"
	const ending = "\n</user_request>"
	if strings.Count(first, boundary) != 1 || strings.Count(first, "<user_request>") != 1 || strings.Count(first, "</user_request>") != 1 || !strings.HasSuffix(first, ending) {
		return "", errParams
	}
	for _, p := range parts[1:] {
		if !strings.HasPrefix(p, "<runtime_info>") {
			return "", errParams
		}
	}
	_, q, _ := strings.Cut(first, boundary)
	return strings.TrimSuffix(q, ending), nil
}
