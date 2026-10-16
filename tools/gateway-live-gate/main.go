// gateway-live-gate is a narrowly scoped worker-side operator tool. It accepts
// no snapshot/config path and keeps kubectl responses exclusively in memory.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const (
	namespace      = "raptor-system"
	secretName     = "gateway-live-config"
	deployment     = "agent-gateway"
	account        = "1360282071200743"
	region         = "ap-southeast-1"
	group          = "asg-t4necb4dfmu6o3z2rn0g"
	serverGroup    = "sgp-xs5s42tihtl6ja2a67"
	cluster        = "c92787e953503492ea141a744c81498f1"
	applicationUID = "17b7b33c-fd64-4d07-863e-f65b6563e504"
)

var rejected = errors.New("gateway live gate rejected; inspect access, idle state, scope and version privately")

type runner interface {
	Run(context.Context, []string, []byte) ([]byte, error)
}
type kubectl struct{}

func (kubectl) Run(ctx context.Context, args []string, input []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--request-timeout=20s", "-n", namespace}, args...)...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stderr = io.Discard
	b, e := cmd.Output()
	if e != nil {
		return nil, rejected
	}
	return b, nil
}
func command(ctx context.Context, r runner, args []string, input []byte) ([]byte, error) {
	b, e := r.Run(ctx, args, input)
	if e != nil {
		return nil, rejected
	}
	return b, nil
}

// decode rejects duplicate keys, trailing input and oversized responses.
func decode(b []byte) (map[string]any, error) {
	if len(b) > 4<<20 {
		return nil, rejected
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	v, e := value(d)
	if e != nil {
		return nil, rejected
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, rejected
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, rejected
	}
	return m, nil
}
func value(d *json.Decoder) (any, error) {
	t, e := d.Token()
	if e != nil {
		return nil, e
	}
	switch t {
	case json.Delim('{'):
		m := map[string]any{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return nil, e
			}
			s, ok := k.(string)
			if !ok {
				return nil, rejected
			}
			if _, ok = m[s]; ok {
				return nil, rejected
			}
			v, e := value(d)
			if e != nil {
				return nil, e
			}
			m[s] = v
		}
		_, e = d.Token()
		return m, e
	case json.Delim('['):
		a := []any{}
		for d.More() {
			v, e := value(d)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		_, e = d.Token()
		return a, e
	}
	return t, nil
}
func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func str(v any) string         { s, _ := v.(string); return s }
func allowed(m map[string]any, keys string) bool {
	if m == nil {
		return false
	}
	for k := range m {
		found := false
		for _, known := range strings.Fields(keys) {
			if k == known {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func schema(c map[string]any) bool {
	if !allowed(c, "qualificationEnabled observationUrl replacementEnabled replacementObserver skillsRepository provider ax infraUsername infraPassword accountId region teamId templateId bucket bucketPrefix volumeName volumeId executionRoleArn bootstrapCheckpoint raptorMcpUrl infraMcpUrl harnessDir") {
		return false
	}
	for k, v := range c {
		switch k {
		case "replacementEnabled", "qualificationEnabled":
			if _, ok := v.(bool); !ok {
				return false
			}
		case "replacementObserver", "ax", "bootstrapCheckpoint":
			if obj(v) == nil {
				return false
			}
		default:
			if _, ok := v.(string); !ok {
				return false
			}
		}
	}
	if v, ok := c["ax"]; ok {
		if !stringObject(obj(v), "endpoint caFile certFile keyFile") {
			return false
		}
	}
	if v, ok := c["bootstrapCheckpoint"]; ok {
		m := obj(v)
		if !allowed(m, "archiveKey checksumKey sha256 bytes piVersion encryption verifiedAt") {
			return false
		}
		for k, v := range m {
			if k == "bytes" {
				if _, ok := v.(json.Number); !ok {
					return false
				}
			} else if _, ok := v.(string); !ok {
				return false
			}
		}
	}
	o := obj(c["replacementObserver"])
	if !allowed(o, "scope clusterServer dbInstanceId loadBalancerId listenerId prometheusUrl clientTargets sqlBucket sqlKey") {
		return false
	}
	for k, v := range o {
		if k == "scope" {
			continue
		}
		if k == "clientTargets" {
			a, ok := v.([]any)
			if !ok {
				return false
			}
			for _, v := range a {
				if _, ok := v.(string); !ok {
					return false
				}
			}
		} else if _, ok := v.(string); !ok {
			return false
		}
	}
	scope := obj(o["scope"])
	if !allowed(scope, "envCode proxyCode groupId serverGroupId targetDbCode application") {
		return false
	}
	want := map[string]string{"envCode": "rdev.ali", "proxyCode": "DP00001", "targetDbCode": "D00002", "groupId": group, "serverGroupId": serverGroup}
	for k, v := range want {
		if scope[k] != v {
			return false
		}
	}
	app := obj(scope["application"])
	if !stringObject(app, "envCode appCode clusterId namespace name uid") {
		return false
	}
	for k, v := range map[string]string{"envCode": "rdev.ali", "appCode": "A00003", "clusterId": cluster, "namespace": "raptor-test", "name": "rdev-test-client-client", "uid": applicationUID} {
		if app[k] != v {
			return false
		}
	}
	_, ok := c["replacementEnabled"].(bool)
	return ok && c["accountId"] == account && c["region"] == region
}
func stringObject(m map[string]any, keys string) bool {
	if !allowed(m, keys) {
		return false
	}
	for _, v := range m {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return true
}
func secretConfig(b []byte) (map[string]any, map[string]any, error) {
	s, e := decode(b)
	if e != nil {
		return nil, nil, rejected
	}
	if !allowed(s, "apiVersion kind metadata type data immutable") || s["apiVersion"] != "v1" || s["kind"] != "Secret" || s["type"] != "Opaque" {
		return nil, nil, rejected
	}
	m := obj(s["metadata"])
	if m["namespace"] != namespace || m["name"] != secretName || str(m["resourceVersion"]) == "" || str(m["uid"]) == "" {
		return nil, nil, rejected
	}
	data := obj(s["data"])
	if len(data) != 1 {
		return nil, nil, rejected
	}
	raw, e := base64.StdEncoding.DecodeString(str(data["config.json"]))
	if e != nil {
		return nil, nil, rejected
	}
	c, e := decode(raw)
	if e != nil || !schema(c) {
		return nil, nil, rejected
	}
	return s, c, nil
}
func getSecret(ctx context.Context, r runner) (map[string]any, map[string]any, error) {
	b, e := command(ctx, r, []string{"get", "secret", secretName, "-o", "json"}, nil)
	if e != nil {
		return nil, nil, e
	}
	return secretConfig(b)
}
func idle(ctx context.Context, r runner) error {
	b, e := command(ctx, r, []string{"exec", "deployment/" + deployment, "-c", deployment, "--", "/usr/local/bin/gateway", "--idle-check"}, nil)
	if e != nil || string(b) != "idle=true\n" {
		return rejected
	}
	return nil
}

type receipt struct {
	ResourceVersion    string    `json:"resourceVersion"`
	ChangedAfter       time.Time `json:"changedAfter"`
	ReplacementEnabled bool      `json:"replacementEnabled"`
}

func setFlag(ctx context.Context, r runner, expected, next bool) (receipt, error) {
	if expected == next {
		return receipt{}, rejected
	}
	if idle(ctx, r) != nil {
		return receipt{}, rejected
	}
	s, c, e := getSecret(ctx, r)
	if e != nil || c["replacementEnabled"] != expected {
		return receipt{}, rejected
	}
	original, _ := json.Marshal(c)
	c["replacementEnabled"] = next
	updated, _ := json.Marshal(c)
	c["replacementEnabled"] = expected
	check, _ := json.Marshal(c)
	if !bytes.Equal(original, check) {
		return receipt{}, rejected
	}
	obj(s["data"])["config.json"] = base64.StdEncoding.EncodeToString(updated)
	payload, _ := json.Marshal(s)
	if idle(ctx, r) != nil {
		return receipt{}, rejected
	}

	b, e := command(ctx, r, []string{"replace", "-f", "-", "-o", "json"}, payload)
	if e != nil {
		return receipt{}, rejected
	}
	result, actual, e := secretConfig(b)
	if e != nil || actual["replacementEnabled"] != next {
		return receipt{}, rejected
	}
	var wanted map[string]any
	wanted, _ = decode(updated)
	if !reflect.DeepEqual(actual, wanted) || obj(result["metadata"])["uid"] != obj(s["metadata"])["uid"] || obj(result["metadata"])["resourceVersion"] == obj(s["metadata"])["resourceVersion"] {
		return receipt{}, rejected
	}
	return receipt{str(obj(result["metadata"])["resourceVersion"]), time.Now().UTC(), next}, nil
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if run(ctx, os.Args[1:], kubectl{}, os.Stdout) != nil {
		fmt.Fprintln(os.Stderr, "gateway live gate rejected")
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string, r runner, out io.Writer) error {
	mode := "set"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		mode = args[0]
		args = args[1:]
	}
	f := flag.NewFlagSet("gateway-live-gate", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	ns := f.String("namespace", namespace, "")
	secret := f.String("secret", secretName, "")
	expected := f.String("expected", "", "")
	next := f.String("set", "", "")
	rv := f.String("resource-version", "", "")
	since := f.String("changed-after", "", "")
	if f.Parse(args) != nil || f.NArg() != 0 || *ns != namespace || *secret != secretName {
		return rejected
	}
	if *expected != "true" && *expected != "false" {
		return rejected
	}
	want, _ := strconv.ParseBool(*expected)
	if mode == "set" {
		if (*next != "true" && *next != "false") || *next == *expected || *rv != "" || *since != "" {
			return rejected
		}
		v, _ := strconv.ParseBool(*next)
		receipt, e := setFlag(ctx, r, want, v)
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(receipt)
	}
	if (mode != "verify" && mode != "rollout") || *next != "" || *rv == "" {
		return rejected
	}
	changed, e := time.Parse(time.RFC3339Nano, *since)
	if e != nil || changed.After(time.Now()) {
		return rejected
	}
	if mode == "rollout" {
		if e := rollout(ctx, r, want, *rv); e != nil {
			return e
		}
		_, e := fmt.Fprintln(out, "rollout=requested")
		return e
	}
	proof, e := verify(ctx, r, want, *rv, changed)
	if e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(proof)
}

func getObject(ctx context.Context, r runner, kind, name string) (map[string]any, error) {
	args := []string{"get", kind}
	if name != "" {
		args = append(args, name)
	}
	args = append(args, "-o", "json")
	b, e := command(ctx, r, args, nil)
	if e != nil {
		return nil, rejected
	}
	return decode(b)
}
func exactVersion(ctx context.Context, r runner, want bool, rv string) (map[string]any, error) {
	s, c, e := getSecret(ctx, r)
	if e != nil || obj(s["metadata"])["resourceVersion"] != rv || c["replacementEnabled"] != want {
		return nil, rejected
	}
	return c, nil
}
func number(v any) int64 {
	n, ok := v.(json.Number)
	if !ok {
		return -1
	}
	i, e := n.Int64()
	if e != nil {
		return -1
	}
	return i
}
func deploymentObject(ctx context.Context, r runner) (map[string]any, error) {
	d, e := getObject(ctx, r, "deployment", deployment)
	m := obj(d["metadata"])
	if e != nil || m["name"] != deployment || m["namespace"] != namespace || str(m["uid"]) == "" || str(m["resourceVersion"]) == "" || number(m["generation"]) < 1 {
		return nil, rejected
	}
	return d, nil
}
func rollout(ctx context.Context, r runner, want bool, rv string) error {
	if _, e := exactVersion(ctx, r, want, rv); e != nil {
		return e
	}
	d, e := deploymentObject(ctx, r)
	if e != nil {
		return e
	}
	// Observe idle immediately before the final Secret version check and patch.
	// Kubernetes cannot transact the database, Secret and Deployment together.
	if idle(ctx, r) != nil {
		return rejected
	}
	if _, e = exactVersion(ctx, r, want, rv); e != nil {
		return e
	}
	spec := obj(d["spec"])
	template := obj(spec["template"])
	metadata := obj(template["metadata"])
	annotations := obj(metadata["annotations"])
	if annotations == nil {
		annotations = map[string]any{}
	}
	annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	patch := []any{map[string]any{"op": "test", "path": "/metadata/resourceVersion", "value": obj(d["metadata"])["resourceVersion"]}, map[string]any{"op": "add", "path": "/spec/template/metadata/annotations", "value": annotations}}
	b, _ := json.Marshal(patch)
	_, e = command(ctx, r, []string{"patch", "deployment", deployment, "--type=json", "-p", string(b)}, nil)
	return e
}
func owner(m map[string]any, kind, uid string) (map[string]any, bool) {
	refs, _ := m["ownerReferences"].([]any)
	for _, v := range refs {
		o := obj(v)
		if o["kind"] == kind && o["controller"] == true && (uid == "" || o["uid"] == uid) {
			return o, true
		}
	}
	return nil, false
}
func readyPod(p map[string]any, since time.Time) bool {
	m := obj(p["metadata"])
	s := obj(p["status"])
	if m["namespace"] != namespace || str(m["name"]) == "" || str(m["uid"]) == "" || m["deletionTimestamp"] != nil || s["phase"] != "Running" {
		return false
	}
	started, e := time.Parse(time.RFC3339Nano, str(s["startTime"]))
	if e != nil || !started.After(since) {
		return false
	}
	conditions, _ := s["conditions"].([]any)
	for _, v := range conditions {
		c := obj(v)
		if c["type"] == "Ready" && c["status"] == "True" {
			return true
		}
	}
	return false
}

type verification struct {
	ReplacementEnabled bool   `json:"replacementEnabled"`
	Generation         int64  `json:"generation"`
	Ready              bool   `json:"ready"`
	PodUID             string `json:"podUID"`
}

func verify(ctx context.Context, r runner, want bool, rv string, since time.Time) (verification, error) {
	c, e := exactVersion(ctx, r, want, rv)
	if e != nil {
		return verification{}, e
	}
	d, e := deploymentObject(ctx, r)
	if e != nil {
		return verification{}, e
	}
	m := obj(d["metadata"])
	status := obj(d["status"])
	generation := number(m["generation"])
	if number(obj(d["spec"])["replicas"]) != 1 || number(status["observedGeneration"]) != generation {
		return verification{}, rejected
	}
	for _, k := range []string{"replicas", "updatedReplicas", "readyReplicas", "availableReplicas"} {
		if number(status[k]) != 1 {
			return verification{}, rejected
		}
	}
	pods, e := getObject(ctx, r, "pods", "")
	if e != nil {
		return verification{}, e
	}
	items, _ := pods["items"].([]any)
	var matched map[string]any
	for _, v := range items {
		p := obj(v)
		pm := obj(p["metadata"])
		ref, ok := owner(pm, "ReplicaSet", "")
		if !ok {
			continue
		}
		rs, e := getObject(ctx, r, "replicaset", str(ref["name"]))
		if e != nil {
			return verification{}, e
		}
		rsm := obj(rs["metadata"])
		_, ours := owner(rsm, "Deployment", str(m["uid"]))
		if !ours {
			continue
		}
		if rsm["uid"] != ref["uid"] || rsm["namespace"] != namespace || matched != nil || !readyPod(p, since) {
			return verification{}, rejected
		}
		matched = p
	}
	if matched == nil {
		return verification{}, rejected
	}
	pm := obj(matched["metadata"])
	effective, e := command(ctx, r, []string{"exec", str(pm["name"]), "-c", deployment, "--", "cat", "/run/raptor/live/config.json"}, nil)
	if e != nil {
		return verification{}, e
	}
	actual, e := decode(effective)
	if e != nil || !reflect.DeepEqual(c, actual) {
		return verification{}, rejected
	}
	// Recheck version and Pod UID after exec to reject a concurrent update/restart.
	if _, e = exactVersion(ctx, r, want, rv); e != nil {
		return verification{}, e
	}
	latest, e := getObject(ctx, r, "pods", "")
	if e != nil {
		return verification{}, e
	}
	found := false
	latestItems, _ := latest["items"].([]any)
	for _, v := range latestItems {
		p := obj(v)
		meta := obj(p["metadata"])
		if meta["name"] == pm["name"] {
			if meta["uid"] != pm["uid"] || !readyPod(p, since) {
				return verification{}, rejected
			}
			found = true
		}
	}
	if !found {
		return verification{}, rejected
	}
	finalDeployment, e := deploymentObject(ctx, r)
	if e != nil || obj(finalDeployment["metadata"])["resourceVersion"] != m["resourceVersion"] {
		return verification{}, rejected
	}
	return verification{want, generation, true, str(pm["uid"])}, nil
}
