package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (c HTTPRaptor) liveRead(ctx context.Context, route string, out any) error {
	u, e := url.Parse(c.BaseURL)
	if e != nil || u.User != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) || c.Username == "" || c.Password == "" {
		return ErrInvalid
	}
	req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(c.BaseURL, "/")+route, nil)
	if e != nil {
		return ErrInvalid
	}
	req.SetBasicAuth(c.Username, c.Password)
	client := http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ErrUnavailable
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if e != nil || len(raw) > 65536 {
		return ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(out) != nil {
		return ErrUnavailable
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return ErrUnavailable
	}
	return nil
}
func (c HTTPRaptor) ResolveLiveDecision(ctx context.Context, requestID string, w LiveWait) (LiveContinuation, error) {
	var control struct {
		Data struct {
			RequestID    string  `json:"requestId"`
			Status       string  `json:"status"`
			ControlState *string `json:"controlState"`
		}
	}
	prefix := "/v1/requests/" + url.PathEscape(requestID)
	if e := c.liveRead(ctx, prefix+"/control", &control); e != nil {
		return LiveContinuation{}, e
	}
	if control.Data.RequestID != requestID || control.Data.ControlState == nil {
		return LiveContinuation{}, ErrInvalid
	}
	current := *control.Data.ControlState
	if current == "cancel_requested" || current == "cancelled" || control.Data.Status == "cancelled" {
		return LiveContinuation{Next: "cancel"}, nil
	}
	if current == "blocked" || control.Data.Status == "blocked" {
		return LiveContinuation{Next: "block"}, nil
	}
	if current != "" || control.Data.Status == "completed" || control.Data.Status == "failed" || control.Data.Status == "" {
		return LiveContinuation{}, ErrInvalid
	}
	if w.Kind == "resource" {
		if !w.Valid() {
			return LiveContinuation{}, ErrInvalid
		}
		if time.Now().Before(w.StartedAt.Add(time.Duration(w.WakeAfterSeconds) * time.Second)) {
			return LiveContinuation{Next: "waiting"}, nil
		}
		return LiveContinuation{Next: "resume"}, nil
	}
	var approval struct{ Data LiveApprovalDecision }
	if e := c.liveRead(ctx, prefix+"/approvals/"+url.PathEscape(w.ApprovalID), &approval); e != nil {
		return LiveContinuation{}, e
	}
	return DecisionForLiveWait(requestID, current, w, approval.Data)
}
