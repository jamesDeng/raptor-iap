package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	sts "github.com/alibabacloud-go/sts-20150401/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
)

type cloudFunction struct{ client *openapi.Client }

func newFunctionAPI(ctx context.Context) (functionAPI, error) {
	key := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	secret := os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	token := os.Getenv("ALIBABA_CLOUD_SECURITY_TOKEN")
	if key == "" || secret == "" || token == "" {
		return nil, errors.New("reviewed deployment requires temporary OIDC credentials")
	}
	config := func(endpoint string) *openapi.Config {
		return &openapi.Config{
			AccessKeyId: tea.String(key), AccessKeySecret: tea.String(secret), SecurityToken: tea.String(token),
			Endpoint: tea.String(endpoint), ConnectTimeout: tea.Int(5000), ReadTimeout: tea.Int(20000),
		}
	}
	identity, err := sts.NewClient(config("sts." + region + ".aliyuncs.com"))
	if err != nil {
		return nil, errors.New("cannot initialize release identity client")
	}
	result, err := identity.GetCallerIdentityWithOptions(&dara.RuntimeOptions{Autoretry: tea.Bool(false)})
	if err != nil || result == nil || result.Body == nil || tea.StringValue(result.Body.AccountId) != accountID ||
		!strings.HasPrefix(tea.StringValue(result.Body.Arn), "acs:ram::"+accountID+":assumed-role/raptor-iap-rdev-apply/") {
		return nil, errors.New("protected rdev apply role could not be verified")
	}
	client, err := openapi.NewClient(config("fcv3." + region + ".aliyuncs.com"))
	if err != nil || ctx.Err() != nil {
		return nil, errors.New("cannot initialize function release client")
	}
	return cloudFunction{client}, nil
}

func (f cloudFunction) call(ctx context.Context, action, method string, body map[string]any) (map[string]any, error) {
	return f.client.CallApiWithCtx(ctx, &openapi.Params{
		Action: tea.String(action), Version: tea.String("2023-03-30"), Protocol: tea.String("HTTPS"),
		Pathname: tea.String("/2023-03-30/functions/" + functionName), Method: tea.String(method),
		AuthType: tea.String("AK"), Style: tea.String("ROA"), ReqBodyType: tea.String("json"), BodyType: tea.String("json"),
	}, &openapi.OpenApiRequest{Body: body}, &dara.RuntimeOptions{Autoretry: tea.Bool(false)})
}

func (f cloudFunction) read(ctx context.Context) (functionState, error) {
	out, err := f.call(ctx, "GetFunction", "GET", nil)
	if err != nil {
		return functionState{}, errors.New("function metadata unavailable")
	}
	var result struct {
		ID           string `json:"functionId"`
		Name         string `json:"functionName"`
		Runtime      string `json:"runtime"`
		Checksum     string `json:"codeChecksum"`
		UpdateStatus string `json:"lastUpdateStatus"`
	}
	body, err := json.Marshal(out["body"])
	if err != nil || json.Unmarshal(body, &result) != nil {
		return functionState{}, errors.New("function metadata invalid")
	}
	return functionState{result.ID, result.Name, result.Runtime, result.Checksum, result.UpdateStatus}, nil
}

func (f cloudFunction) update(ctx context.Context, data []byte) error {
	_, err := f.call(ctx, "UpdateFunction", "PUT", map[string]any{
		"code": map[string]any{"zipFile": base64.StdEncoding.EncodeToString(data)},
	})
	return err
}
