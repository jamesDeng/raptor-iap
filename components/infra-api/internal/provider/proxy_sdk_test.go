package provider

import (
	"context"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProxyActualSDKScaleDoesNotResubmitOnFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledged", true: "transport failure"}[fail], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				r.ParseForm()
				if r.Header.Get("x-acs-action") != "ModifyScalingGroup" || r.Form.Get("ScalingGroupId") != "asg-test" || r.Form.Get("DesiredCapacity") != "3" || r.Form.Get("MinSize") != "" || r.Form.Get("MaxSize") != "" {
					t.Error("incorrect cloud mutation")
				}
				w.Header().Set("Content-Type", "application/json")
				if fail {
					w.WriteHeader(503)
					w.Write([]byte(`{"Code":"ServiceUnavailable","Message":"private provider error","RequestId":"error-1"}`))
				} else {
					w.Write([]byte(`{"RequestId":"scale-1"}`))
				}
			}))
			defer server.Close()
			config := func(string) *openapi.Config {
				return &openapi.Config{Endpoint: tea.String(strings.TrimPrefix(server.URL, "http://")), Protocol: tea.String("http"), AccessKeyId: tea.String("fixture"), AccessKeySecret: tea.String("fixture"), ReadTimeout: tea.Int(1000)}
			}
			sdk, e := cloudProxy(config)
			if e != nil {
				t.Fatal(e)
			}
			p, env, _, _, _, _ := proxyFixture(t)
			p.sdk.scale = sdk.scale
			ack, e := p.Scale(context.Background(), env, "asg-test", 3)
			if fail {
				if e == nil || ack != "" || strings.Contains(e.Error(), "private") {
					t.Fatal("provider failure leaked or falsely succeeded")
				}
			} else if e != nil || ack != "scale-1" {
				t.Fatalf("ack=%s error=%v", ack, e)
			}
			if calls.Load() != 1 {
				t.Fatalf("mutation sent %d times", calls.Load())
			}
		})
	}
}
