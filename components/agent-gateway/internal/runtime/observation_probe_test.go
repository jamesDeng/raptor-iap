package runtime

import (
	"context"
	"encoding/json"
	"errors"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"io"
	"net/http"
	"net/url"
	"testing"
)

func TestObserverProbeRejectsWritesForeignSelectorsAndSQL(t *testing.T) {
	config := json.RawMessage(`{"region":"ap-southeast-1","groupId":"asg-test","serverGroupId":"sgp-test","listenerId":"lsn-test","application":{"namespace":"raptor-test","name":"client"},"private":{"sql":{"user":"app","password":"private","database":"test","sslmode":"disable"}}}`)
	bad := []ObserverProbeInput{
		{Command: "sh", Args: []string{"-c", "echo bad"}},
		{Command: "aliyun", Args: []string{"ess", "ModifyScalingGroup", "--RegionId", "ap-southeast-1", "--ScalingGroupId", "asg-test"}},
		{Command: "aliyun", Args: []string{"ess", "DescribeScalingInstances", "--RegionId", "ap-southeast-1", "--ScalingGroupId", "asg-foreign", "--PageSize", "50"}},
		{Command: "kubectl", Args: []string{"--kubeconfig", "/tmp/raptor-private/kubeconfig", "--namespace", "raptor-test", "get", "secrets", "--selector", "app=client", "--output", "json"}},
		{Command: "psql", Args: []string{"--command", "DELETE FROM test"}},
	}
	for _, input := range bad {
		input.Config = config
		if _, e := ExecuteObserverProbe(context.Background(), input); e == nil {
			t.Fatal("unsafe probe accepted", input.Command)
		}
	}
}
func TestObserverCloudPlansAllowOnlyExactBoundReads(t *testing.T) {
	input := ObserverProbeInput{Command: "aliyun", Config: json.RawMessage(`{"region":"ap-southeast-1","groupId":"asg-test","serverGroupId":"sgp-test","listenerId":"lsn-test"}`), Args: []string{"ess", "DescribeScalingGroups", "--RegionId", "ap-southeast-1", "--ScalingGroupIds", `["asg-test"]`}}
	plan, e := observerCloudPlan(input)
	if e != nil || plan.Action != "DescribeScalingGroups" || plan.Host != "ess.ap-southeast-1.aliyuncs.com" || plan.Version != "2022-02-22" || plan.Query["ScalingGroupIds.1"] == nil || *plan.Query["ScalingGroupIds.1"] != "asg-test" {
		t.Fatal("bound read rejected", e)
	}
	input.Args = append(input.Args, "--Force", "true")
	if _, e := observerCloudPlan(input); e == nil {
		t.Fatal("extra option accepted")
	}
}

func TestObserverNLBRequestsMatchOfficialParameterPlacement(t *testing.T) {
	for _, action := range []string{"GetListenerAttribute", "GetListenerHealthStatus", "ListServerGroupServers"} {
		plan := cloudReadPlan{Host: "nlb.ap-southeast-1.aliyuncs.com", Action: action, Query: map[string]*string{}}
		request := plan.request()
		if action == "ListServerGroupServers" {
			if request.Body == nil || request.Query != nil {
				t.Fatal("list requires form body")
			}
		} else if request.Query == nil || request.Body != nil {
			t.Fatal("listener requires query", action)
		}
	}
}

type observationWireCapture struct {
	request *http.Request
	body    url.Values
}

func (c *observationWireCapture) Call(r *http.Request, _ *http.Transport) (*http.Response, error) {
	c.request = r
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		c.body, _ = url.ParseQuery(string(raw))
	}
	return nil, errors.New("intercepted before network")
}
func TestObserverNLBHTTPWire(t *testing.T) {
	for _, action := range []string{"GetListenerAttribute", "GetListenerHealthStatus", "ListServerGroupServers"} {
		plan := cloudReadPlan{Host: "nlb.ap-southeast-1.aliyuncs.com", Version: "2022-04-30", Action: action, Query: map[string]*string{"RegionId": dara.String("ap-southeast-1"), "ListenerId": dara.String("lsn-test")}}
		capture := &observationWireCapture{}
		client, e := openapi.NewClient(&openapi.Config{AccessKeyId: dara.String("synthetic"), AccessKeySecret: dara.String("synthetic"), Endpoint: dara.String(plan.Host), Protocol: dara.String("HTTPS")})
		if e != nil {
			t.Fatal(e)
		}
		client.HttpClient = capture
		_, _ = client.CallApi(&openapi.Params{Action: dara.String(action), Version: dara.String(plan.Version), Protocol: dara.String("HTTPS"), Pathname: dara.String("/"), Method: dara.String("POST"), AuthType: dara.String("AK"), Style: dara.String("RPC"), ReqBodyType: dara.String("formData"), BodyType: dara.String("json")}, plan.request(), sdkOptions())
		if capture.request == nil {
			t.Fatal("no intercepted request")
		}
		query := capture.request.URL.Query()
		if action == "ListServerGroupServers" {
			if capture.body.Get("ListenerId") != "lsn-test" || query.Get("ListenerId") != "" {
				t.Fatalf("wrong list wire: query=%v body=%v content-type=%v", query, capture.body, capture.request.Header.Get("Content-Type"))
			}
		} else if query.Get("ListenerId") != "lsn-test" || capture.body.Get("ListenerId") != "" {
			t.Fatal("wrong listener wire", action)
		}
	}
}
