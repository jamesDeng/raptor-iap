package runtime

import (
	"context"
	"encoding/json"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"reflect"
)

type ObserverProbeInput struct {
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env"`
	Kubeconfig json.RawMessage   `json:"kubeconfig"`
	Config     json.RawMessage   `json:"config"`
}
type observerConfig struct {
	Region, GroupID, ServerGroupID, ListenerID string
	Application                                struct{ Namespace, Name string }
	Private                                    struct {
		SQL struct{ User, Password, Database, SSLMode string }
	}
}

func observerConfigFrom(in ObserverProbeInput) (observerConfig, error) {
	var c observerConfig
	if json.Unmarshal(in.Config, &c) != nil || c.Region != "ap-southeast-1" {
		return c, ErrConfiguration
	}
	return c, nil
}

type cloudReadPlan struct {
	Host, Version, Action string
	Query                 map[string]*string
}

func observerCloudPlan(in ObserverProbeInput) (cloudReadPlan, error) {
	c, e := observerConfigFrom(in)
	if e != nil || in.Command != "aliyun" {
		return cloudReadPlan{}, ErrConfiguration
	}
	groupJSON, _ := json.Marshal([]string{c.GroupID})
	cases := []struct {
		args          []string
		host, version string
		query         map[string]*string
	}{
		{[]string{"sts", "GetCallerIdentity", "--RegionId", c.Region}, "sts.ap-southeast-1.aliyuncs.com", "2015-04-01", nil},
		{[]string{"ess", "DescribeScalingInstances", "--RegionId", c.Region, "--ScalingGroupId", c.GroupID, "--PageSize", "50"}, "ess.ap-southeast-1.aliyuncs.com", "2022-02-22", map[string]*string{"RegionId": dara.String(c.Region), "ScalingGroupId": dara.String(c.GroupID), "PageSize": dara.String("50")}},
		{[]string{"ess", "DescribeScalingGroups", "--RegionId", c.Region, "--ScalingGroupIds", string(groupJSON)}, "ess.ap-southeast-1.aliyuncs.com", "2022-02-22", map[string]*string{"RegionId": dara.String(c.Region), "ScalingGroupIds.1": dara.String(c.GroupID)}},
		{[]string{"nlb", "ListServerGroupServers", "--RegionId", c.Region, "--ServerGroupId", c.ServerGroupID}, "nlb.ap-southeast-1.aliyuncs.com", "2022-04-30", map[string]*string{"RegionId": dara.String(c.Region), "ServerGroupId": dara.String(c.ServerGroupID)}},
		{[]string{"nlb", "GetListenerAttribute", "--RegionId", c.Region, "--ListenerId", c.ListenerID}, "nlb.ap-southeast-1.aliyuncs.com", "2022-04-30", map[string]*string{"RegionId": dara.String(c.Region), "ListenerId": dara.String(c.ListenerID)}},
		{[]string{"nlb", "GetListenerHealthStatus", "--RegionId", c.Region, "--ListenerId", c.ListenerID}, "nlb.ap-southeast-1.aliyuncs.com", "2022-04-30", map[string]*string{"RegionId": dara.String(c.Region), "ListenerId": dara.String(c.ListenerID)}},
	}
	for _, v := range cases {
		if reflect.DeepEqual(in.Args, v.args) {
			return cloudReadPlan{Host: v.host, Version: v.version, Action: in.Args[1], Query: v.query}, nil
		}
	}
	return cloudReadPlan{}, ErrConfiguration
}
func ExecuteObserverProbe(ctx context.Context, in ObserverProbeInput) (json.RawMessage, error) {
	switch in.Command {
	case "aliyun":
		plan, e := observerCloudPlan(in)
		if e != nil {
			return nil, e
		}
		keys := []string{"ALIBABA_CLOUD_ACCESS_KEY_ID", "ALIBABA_CLOUD_ACCESS_KEY_SECRET", "ALIBABA_CLOUD_SECURITY_TOKEN"}
		if len(in.Env) != 3 {
			return nil, ErrConfiguration
		}
		for _, key := range keys {
			if in.Env[key] == "" {
				return nil, ErrConfiguration
			}
		}
		client, e := openapi.NewClient(&openapi.Config{AccessKeyId: dara.String(in.Env[keys[0]]), AccessKeySecret: dara.String(in.Env[keys[1]]), SecurityToken: dara.String(in.Env[keys[2]]), Type: dara.String("sts"), RegionId: dara.String("ap-southeast-1"), Endpoint: dara.String(plan.Host), Protocol: dara.String("HTTPS")})
		if e != nil {
			return nil, ErrRuntimeUnavailable
		}
		client.HttpClient = guardedSDKHTTP{ctx: ctx, origin: plan.Host}
		request := plan.request()
		response, e := client.CallApi(&openapi.Params{Action: dara.String(plan.Action), Version: dara.String(plan.Version), Protocol: dara.String("HTTPS"), Pathname: dara.String("/"), Method: dara.String("POST"), AuthType: dara.String("AK"), Style: dara.String("RPC"), ReqBodyType: dara.String("formData"), BodyType: dara.String("json")}, request, sdkOptions())
		if e != nil {
			return nil, ErrRuntimeUnavailable
		}
		raw, e := json.Marshal(response["body"])
		if e != nil || len(raw) > 262144 {
			return nil, ErrRuntimeUnavailable
		}
		return raw, nil
	case "kubectl":
		return observerKubeRead(ctx, in)
	case "psql":
		return observerSQLRead(ctx, in)
	default:
		return nil, ErrConfiguration
	}
}

func (plan cloudReadPlan) request() *openapi.OpenApiRequest {
	if plan.Action == "ListServerGroupServers" {
		body := map[string]interface{}{}
		for k, v := range plan.Query {
			body[k] = dara.StringValue(v)
		}
		return &openapi.OpenApiRequest{Body: body}
	}
	return &openapi.OpenApiRequest{Query: plan.Query}
}
