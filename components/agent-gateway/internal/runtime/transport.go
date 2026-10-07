package runtime

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrConfiguration = errors.New("InvalidRuntimeConfiguration")
var ErrCreateUnconfirmed = errors.New("SandboxCreateUnconfirmed")
var ErrCommandUnconfirmed = errors.New("CommandStartUnconfirmed")
var ErrInvalidRuntimeData = errors.New("InvalidRuntimeData")
var ErrRuntimeUnavailable = errors.New("RuntimeUnavailable")
var ErrTerminationUnconfirmed = errors.New("TerminationUnconfirmed")
var ErrFileNotFound = errors.New("RuntimeFileNotFound")
var transportID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

type SandboxTransport struct {
	before              func(context.Context) error
	apiURL, domain, key string
	client              *http.Client
	envdFixtureURL      string
}
type CreateSpec struct {
	Template, AttemptID, VolumeName, ExecutionRole string
	TTL                                            int
}
type SandboxRef struct {
	ID          string `json:"id"`
	EnvdVersion string `json:"envdVersion"`
	accessToken string
}
type SandboxState struct {
	Absent bool
	ID     string
}
type CommandSpec struct {
	Executable string
	Args       []string
	Tag        string
	Deadline   time.Duration
}
type CommandRef struct {
	PID    uint32
	Tag    string
	stream io.ReadCloser
	cancel context.CancelFunc
}

func NewSandboxTransport(api, domain, key string, rt http.RoundTripper) (*SandboxTransport, error) {
	u, e := url.Parse(api)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || domain != "ap-southeast-1.e2b.fc.aliyuncs.com" || key == "" {
		return nil, ErrConfiguration
	}
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &SandboxTransport{apiURL: api, domain: domain, key: key, client: &http.Client{Transport: rt, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (t *SandboxTransport) request(ctx context.Context, method, endpoint string, body []byte, headers http.Header) (*http.Response, error) {
	if t.before != nil {
		if e := t.before(ctx); e != nil {
			return nil, e
		}
	}
	r, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if e != nil {
		return nil, ErrConfiguration
	}
	r.Header = headers.Clone()
	resp, e := t.client.Do(r)
	if e != nil {
		return nil, ErrRuntimeUnavailable
	}
	return resp, nil
}
func bounded(body io.Reader, max int64) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(body, max+1))
	if e != nil || int64(len(b)) > max {
		return nil, ErrInvalidRuntimeData
	}
	return b, nil
}
func (t *SandboxTransport) control(ctx context.Context, method, route string, body []byte) (int, []byte, error) {
	h := http.Header{"X-Api-Key": []string{t.key}, "Content-Type": []string{"application/json"}}
	r, e := t.request(ctx, method, t.apiURL+route, body, h)
	if e != nil {
		return 0, nil, e
	}
	defer r.Body.Close()
	b, e := bounded(r.Body, 65536)
	return r.StatusCode, b, e
}
func (t *SandboxTransport) decodeRef(raw []byte, expected string) (SandboxRef, error) {
	var v struct {
		SandboxID string `json:"sandboxID"`
		Domain    string `json:"domain"`
		Version   string `json:"envdVersion"`
		Token     string `json:"envdAccessToken"`
	}
	if json.Unmarshal(raw, &v) != nil || !transportID.MatchString(v.SandboxID) || expected != "" && expected != v.SandboxID || v.Domain != "" && v.Domain != t.domain || v.Version == "" {
		return SandboxRef{}, ErrInvalidRuntimeData
	}
	return SandboxRef{ID: v.SandboxID, EnvdVersion: v.Version, accessToken: v.Token}, nil
}
func (t *SandboxTransport) Create(ctx context.Context, s CreateSpec) (SandboxRef, error) {
	if !transportID.MatchString(s.Template) || !transportID.MatchString(s.AttemptID) || s.VolumeName == "" || s.ExecutionRole == "" || s.TTL < 1 || s.TTL > 900 {
		return SandboxRef{}, ErrConfiguration
	}
	body, _ := json.Marshal(map[string]any{"templateID": s.Template, "timeout": s.TTL, "metadata": map[string]string{"fc.sandbox.auth.role": s.ExecutionRole, "raptor.attempt": s.AttemptID}, "volumeMounts": []map[string]string{{"name": s.VolumeName, "path": "/mnt/oss"}}, "secure": true, "allow_internet_access": true, "autoPause": false, "autoResume": map[string]bool{"enabled": false}, "envVars": map[string]string{}})
	code, raw, e := t.control(ctx, "POST", "/sandboxes", body)
	if e != nil || code != 201 {
		return SandboxRef{}, ErrCreateUnconfirmed
	}
	ref, e := t.decodeRef(raw, "")
	if e != nil {
		return SandboxRef{}, ErrCreateUnconfirmed
	}
	return ref, nil
}
func (t *SandboxTransport) Connect(ctx context.Context, s SandboxRef) (SandboxRef, error) {
	if !transportID.MatchString(s.ID) {
		return SandboxRef{}, ErrConfiguration
	}
	code, raw, e := t.control(ctx, "POST", "/sandboxes/"+s.ID+"/connect", []byte(`{}`))
	if e != nil || code != 200 {
		return SandboxRef{}, ErrRuntimeUnavailable
	}
	return t.decodeRef(raw, s.ID)
}
func (t *SandboxTransport) envd(s SandboxRef) (string, http.Header, error) {
	if !transportID.MatchString(s.ID) {
		return "", nil, ErrConfiguration
	}
	endpoint := "https://49983-" + s.ID + "." + t.domain
	if t.envdFixtureURL != "" {
		endpoint = t.envdFixtureURL
	}
	h := http.Header{"E2b-Sandbox-Id": []string{s.ID}, "E2b-Sandbox-Port": []string{"49983"}, "Authorization": []string{"Basic dXNlcjo="}}
	if s.accessToken != "" {
		h.Set("X-Access-Token", s.accessToken)
	}
	return endpoint, h, nil
}
func runtimePath(p string) bool {
	return strings.HasPrefix(p, "/tmp/") || strings.HasPrefix(p, "/mnt/oss/")
}
func (t *SandboxTransport) WriteFile(ctx context.Context, s SandboxRef, p string, data []byte) error {
	if !runtimePath(p) || path.Clean(p) != p || len(data) > 16*1024*1024 {
		return ErrConfiguration
	}
	endpoint, h, e := t.envd(s)
	if e != nil {
		return e
	}
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
	part, e := writer.CreateFormFile("file", p)
	if e != nil {
		return ErrConfiguration
	}
	part.Write(data)
	writer.Close()
	h.Set("Content-Type", writer.FormDataContentType())
	resp, e := t.request(ctx, "POST", endpoint+"/files?path="+url.QueryEscape(p)+"&username=user", b.Bytes(), h)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ErrRuntimeUnavailable
	}
	_, e = bounded(resp.Body, 65536)
	return e
}
func (t *SandboxTransport) ReadFile(ctx context.Context, s SandboxRef, p string, maxBytes int64) ([]byte, error) {
	if !runtimePath(p) || path.Clean(p) != p || maxBytes < 1 || maxBytes > 16*1024*1024 {
		return nil, ErrConfiguration
	}
	endpoint, h, e := t.envd(s)
	if e != nil {
		return nil, e
	}
	resp, e := t.request(ctx, "GET", endpoint+"/files?path="+url.QueryEscape(p)+"&username=user", nil, h)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, ErrFileNotFound
	}
	if resp.StatusCode != 200 {
		return nil, ErrRuntimeUnavailable
	}
	return bounded(resp.Body, maxBytes)
}
func fmtDuration(d time.Duration) string { return strconv.FormatInt(d.Milliseconds(), 10) }
func connectFrame(raw []byte) []byte {
	b := make([]byte, len(raw)+5)
	binary.BigEndian.PutUint32(b[1:5], uint32(len(raw)))
	copy(b[5:], raw)
	return b
}
func readConnectFrame(r io.Reader) (byte, []byte, error) {
	var h [5]byte
	if _, e := io.ReadFull(r, h[:]); e != nil {
		return 0, nil, ErrInvalidRuntimeData
	}
	n := binary.BigEndian.Uint32(h[1:])
	if n == 0 || n > 65536 || h[0] != 0 && h[0] != 2 {
		return 0, nil, ErrInvalidRuntimeData
	}
	b := make([]byte, n)
	if _, e := io.ReadFull(r, b); e != nil {
		return 0, nil, ErrInvalidRuntimeData
	}
	return h[0], b, nil
}
func (t *SandboxTransport) StartCommand(ctx context.Context, s SandboxRef, c CommandSpec) (CommandRef, error) {
	if t.before != nil {
		if e := t.before(ctx); e != nil {
			return CommandRef{}, e
		}
	}
	if c.Executable == "" || c.Deadline <= 0 || c.Deadline > 5*time.Minute || !transportID.MatchString(c.Tag) {
		return CommandRef{}, ErrConfiguration
	}
	endpoint, h, e := t.envd(s)
	if e != nil {
		return CommandRef{}, e
	}
	raw, _ := json.Marshal(map[string]any{"process": map[string]any{"cmd": c.Executable, "args": c.Args}, "tag": c.Tag, "stdin": false})
	h.Set("Content-Type", "application/connect+json")
	h.Set("Connect-Protocol-Version", "1")
	h.Set("Connect-Timeout-Ms", fmtDuration(c.Deadline))
	commandCtx, cancel := context.WithTimeout(ctx, c.Deadline)
	req, reqErr := http.NewRequestWithContext(commandCtx, "POST", endpoint+"/process.Process/Start", bytes.NewReader(connectFrame(raw)))
	if reqErr != nil {
		cancel()
		return CommandRef{}, ErrCommandUnconfirmed
	}
	req.Header = h
	client := &http.Client{Transport: t.client.Transport, CheckRedirect: t.client.CheckRedirect}
	resp, e := client.Do(req)
	if e != nil {
		cancel()
		return CommandRef{}, ErrCommandUnconfirmed
	}
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/connect+json") {
		resp.Body.Close()
		cancel()
		return CommandRef{}, ErrCommandUnconfirmed
	}
	flag, b, e := readConnectFrame(resp.Body)
	var event struct {
		Event struct {
			Start *struct {
				PID uint32 `json:"pid"`
			} `json:"start"`
		} `json:"event"`
	}
	if e != nil || flag != 0 || json.Unmarshal(b, &event) != nil || event.Event.Start == nil || event.Event.Start.PID == 0 {
		resp.Body.Close()
		cancel()
		return CommandRef{}, ErrCommandUnconfirmed
	}
	return CommandRef{PID: event.Event.Start.PID, Tag: c.Tag, stream: resp.Body, cancel: cancel}, nil
}
func (c *CommandRef) Close() {
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	if c.stream != nil {
		c.stream.Close()
		c.stream = nil
	}
}
func (t *SandboxTransport) Inspect(ctx context.Context, s SandboxRef) (SandboxState, error) {
	if !transportID.MatchString(s.ID) {
		return SandboxState{}, ErrConfiguration
	}
	code, raw, e := t.control(ctx, "GET", "/sandboxes/"+s.ID, nil)
	if e != nil {
		return SandboxState{}, e
	}
	if code == 404 {
		return SandboxState{ID: s.ID, Absent: true}, nil
	}
	var v struct {
		ID string `json:"sandboxID"`
	}
	if code != 200 || json.Unmarshal(raw, &v) != nil || v.ID != s.ID {
		return SandboxState{}, ErrRuntimeUnavailable
	}
	return SandboxState{ID: s.ID}, nil
}
func (t *SandboxTransport) Terminate(ctx context.Context, s SandboxRef) error {
	if !transportID.MatchString(s.ID) {
		return ErrConfiguration
	}
	code, _, e := t.control(ctx, "DELETE", "/sandboxes/"+s.ID, nil)
	if e != nil || code != 204 && code != 200 && code != 404 {
		return ErrTerminationUnconfirmed
	}
	state, e := t.Inspect(ctx, s)
	if e != nil || !state.Absent {
		return ErrTerminationUnconfirmed
	}
	return nil
}

// Inventory is complete or fails closed; duplicate identities/tokens and wrong
// metadata make an uncertain create unreconcilable rather than authorize retry.
func (t *SandboxTransport) InventorySandboxes(ctx context.Context, attempt string) ([]SandboxRef, error) {
	if !transportID.MatchString(attempt) {
		return nil, ErrConfiguration
	}
	out := []SandboxRef{}
	ids, tokens := map[string]bool{}, map[string]bool{}
	token := ""
	for page := 0; page < 100; page++ {
		q := url.Values{"metadata": []string{"raptor.attempt=" + attempt}, "limit": []string{"100"}}
		if token != "" {
			q.Set("nextToken", token)
		}
		resp, e := t.request(ctx, "GET", t.apiURL+"/v2/sandboxes?"+q.Encode(), nil, http.Header{"X-Api-Key": []string{t.key}})
		if e != nil {
			return nil, ErrRuntimeUnavailable
		}
		raw, e := bounded(resp.Body, 65536)
		resp.Body.Close()
		if e != nil || resp.StatusCode != 200 {
			return nil, ErrRuntimeUnavailable
		}
		var rows []struct {
			ID       string            `json:"sandboxID"`
			Version  string            `json:"envdVersion"`
			Metadata map[string]string `json:"metadata"`
		}
		if json.Unmarshal(raw, &rows) != nil || len(rows) > 100 {
			return nil, ErrInvalidRuntimeData
		}
		for _, r := range rows {
			if !transportID.MatchString(r.ID) || ids[r.ID] || r.Metadata["raptor.attempt"] != attempt {
				return nil, ErrInvalidRuntimeData
			}
			ids[r.ID] = true
			out = append(out, SandboxRef{ID: r.ID, EnvdVersion: r.Version})
		}
		token = resp.Header.Get("X-Next-Token")
		if token == "" {
			return out, nil
		}
		if tokens[token] || len(rows) == 0 || len(token) > 4096 {
			return nil, ErrInvalidRuntimeData
		}
		tokens[token] = true
	}
	return nil, ErrRuntimeUnavailable
}
func (c *CommandRef) Wait() error {
	defer c.Close()
	if c.stream == nil {
		return ErrCommandUnconfirmed
	}
	ended := false
	for n := 0; n < 4096; n++ {
		flag, raw, e := readConnectFrame(c.stream)
		if e != nil {
			return ErrCommandUnconfirmed
		}
		if flag == 2 {
			var end struct {
				Error json.RawMessage `json:"error"`
			}
			if json.Unmarshal(raw, &end) != nil || len(end.Error) > 0 && !bytes.Equal(end.Error, []byte("null")) || !ended {
				return ErrCommandUnconfirmed
			}
			return nil
		}
		var v struct {
			Event struct {
				End *struct {
					ExitCode int    `json:"exitCode"`
					Exited   bool   `json:"exited"`
					Error    string `json:"error"`
				} `json:"end"`
			} `json:"event"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return ErrCommandUnconfirmed
		}
		if v.Event.End != nil {
			if v.Event.End.ExitCode != 0 || !v.Event.End.Exited || v.Event.End.Error != "" {
				return ErrCommandUnconfirmed
			}
			ended = true
		}
	}
	return ErrCommandUnconfirmed
}
