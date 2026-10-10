package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"regexp"
	"time"
)

type ObserverCredential struct {
	AccessKeyID     string    `json:"accessKeyId"`
	AccessKeySecret string    `json:"accessKeySecret"`
	SecurityToken   string    `json:"securityToken"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

func (ObserverCredential) String() string     { return "[observer credential]" }
func (c ObserverCredential) GoString() string { return c.String() }

type ObserverSessionRequest struct {
	RoleARN, SessionName, Policy string
	DurationSeconds              int
}
type ObserverCredentialIssuer struct {
	AccountID                      string
	BackendGroupID, LoadBalancerID string
	Source                         *lockedCredential
	Assume                         func(context.Context, ObserverSessionRequest) (*ObserverCredential, error)
}

func (i ObserverCredentialIssuer) Issue(ctx context.Context, groupID, attempt string) (ObserverCredential, error) {
	if ctx.Err() != nil || i.Source == nil || i.AccountID != "1360282071200743" || !regexp.MustCompile(`^asg-[A-Za-z0-9]+$`).MatchString(groupID) || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,57}$`).MatchString(attempt) {
		return ObserverCredential{}, ErrConfiguration
	}
	if (i.BackendGroupID == "") != (i.LoadBalancerID == "") {
		return ObserverCredential{}, ErrConfiguration
	}
	statements := []any{map[string]any{"Effect": "Allow", "Action": []string{"ess:DescribeScalingInstances", "ess:DescribeScalingGroups"}, "Resource": []string{fmt.Sprintf("acs:ess:ap-southeast-1:%s:scalinggroup/%s", i.AccountID, groupID)}}}
	if i.BackendGroupID != "" {
		if !regexp.MustCompile(`^sgp-[A-Za-z0-9]+$`).MatchString(i.BackendGroupID) || !regexp.MustCompile(`^nlb-[A-Za-z0-9]+$`).MatchString(i.LoadBalancerID) {
			return ObserverCredential{}, ErrConfiguration
		}
		statements = append(statements, map[string]any{"Effect": "Allow", "Action": []string{"nlb:ListServerGroupServers"}, "Resource": []string{fmt.Sprintf("acs:nlb:ap-southeast-1:%s:serverGroup/%s", i.AccountID, i.BackendGroupID)}}, map[string]any{"Effect": "Allow", "Action": []string{"nlb:GetListenerAttribute", "nlb:GetListenerHealthStatus"}, "Resource": []string{fmt.Sprintf("acs:nlb:ap-southeast-1:%s:loadbalancer/%s", i.AccountID, i.LoadBalancerID)}})
	}
	policy, _ := json.Marshal(map[string]any{"Version": "1", "Statement": statements})
	request := ObserverSessionRequest{RoleARN: "acs:ram::" + i.AccountID + ":role/raptor-rdev-agent-observer", SessionName: "raptor-" + attempt, Policy: string(policy), DurationSeconds: 900}
	assume := i.Assume
	if assume == nil {
		assume = i.assume
	}
	credential, err := assume(ctx, request)
	if err != nil || ctx.Err() != nil || credential == nil || credential.AccessKeyID == "" || credential.AccessKeySecret == "" || credential.SecurityToken == "" || credential.ExpiresAt.Before(time.Now().Add(120*time.Second)) || credential.ExpiresAt.After(time.Now().Add(930*time.Second)) {
		return ObserverCredential{}, ErrRuntimeUnavailable
	}
	return *credential, nil
}
func (i ObserverCredentialIssuer) assume(ctx context.Context, r ObserverSessionRequest) (*ObserverCredential, error) {
	client, err := openapi.NewClient(&openapi.Config{Credential: i.Source, RegionId: dara.String("ap-southeast-1"), Endpoint: dara.String("sts.ap-southeast-1.aliyuncs.com"), Protocol: dara.String("HTTPS")})
	if err != nil {
		return nil, ErrRuntimeUnavailable
	}
	client.HttpClient = guardedSDKHTTP{ctx: ctx, origin: "sts.ap-southeast-1.aliyuncs.com"}
	return observerSDKAssume(client, r)
}
func observerSDKAssume(client *openapi.Client, r ObserverSessionRequest) (*ObserverCredential, error) {
	params := &openapi.Params{Action: dara.String("AssumeRole"), Version: dara.String("2015-04-01"), Protocol: dara.String("HTTPS"), Pathname: dara.String("/"), Method: dara.String("POST"), AuthType: dara.String("AK"), Style: dara.String("RPC"), ReqBodyType: dara.String("formData"), BodyType: dara.String("json")}
	response, err := client.CallApi(params, &openapi.OpenApiRequest{Query: map[string]*string{"RoleArn": dara.String(r.RoleARN), "RoleSessionName": dara.String(r.SessionName), "DurationSeconds": dara.String("900"), "Policy": dara.String(r.Policy)}}, sdkOptions())
	if err != nil {
		return nil, ErrRuntimeUnavailable
	}
	body, ok := response["body"].(map[string]interface{})
	if !ok {
		return nil, ErrRuntimeUnavailable
	}
	values, ok := body["Credentials"].(map[string]interface{})
	if !ok {
		return nil, ErrRuntimeUnavailable
	}
	text := func(key string) string { value, _ := values[key].(string); return value }
	expires, err := time.Parse(time.RFC3339, text("Expiration"))
	if err != nil {
		return nil, ErrRuntimeUnavailable
	}
	return &ObserverCredential{AccessKeyID: text("AccessKeyId"), AccessKeySecret: text("AccessKeySecret"), SecurityToken: text("SecurityToken"), ExpiresAt: expires}, nil
}

// ObserverIssuer reuses the explicit, reviewed controller source; it never
// searches ambient credentials or places the controller credential in a job.
func (m Management) ObserverIssuer(backendGroupID, loadBalancerID string) (ObserverCredentialIssuer, error) {
	if m.credential == nil || m.AccountID != "1360282071200743" || !regexp.MustCompile(`^sgp-[A-Za-z0-9]+$`).MatchString(backendGroupID) || !regexp.MustCompile(`^nlb-[A-Za-z0-9]+$`).MatchString(loadBalancerID) {
		return ObserverCredentialIssuer{}, ErrConfiguration
	}
	return ObserverCredentialIssuer{AccountID: m.AccountID, BackendGroupID: backendGroupID, LoadBalancerID: loadBalancerID, Source: m.credential}, nil
}
