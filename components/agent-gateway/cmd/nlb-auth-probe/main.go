// Permission-only diagnostic; no caller-supplied action, target or DryRun flag.
package main

import (
	"encoding/json"
	"fmt"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"os"
)

func probe(c *openapi.Client, kind, id string) (map[string]interface{}, error) {
	if (kind != "servergroup" || id != "sgp-xs5s42tihtl6ja2a67") && (kind != "loadbalancer" || id != "nlb-jqbxi42nurq0qkb7e9") {
		return nil, fmt.Errorf("unexpected target")
	}
	return c.CallApi(&openapi.Params{Action: dara.String("UntagResources"), Version: dara.String("2022-04-30"), Protocol: dara.String("HTTPS"), Pathname: dara.String("/"), Method: dara.String("POST"), AuthType: dara.String("AK"), Style: dara.String("RPC"), ReqBodyType: dara.String("formData"), BodyType: dara.String("json")}, &openapi.OpenApiRequest{Body: map[string]interface{}{"DryRun": true, "All": false, "RegionId": "ap-southeast-1", "ResourceType": kind, "ResourceId.1": id, "TagKey.1": "db-proxy-code"}}, &dara.RuntimeOptions{Autoretry: dara.Bool(false), ReadTimeout: dara.Int(10000), ConnectTimeout: dara.Int(5000)})
}
func main() {
	for _, k := range []string{"ALIBABA_CLOUD_ACCESS_KEY_ID", "ALIBABA_CLOUD_ACCESS_KEY_SECRET", "ALIBABA_CLOUD_SECURITY_TOKEN", "PROBE_PRIVATE_OUTPUT"} {
		if os.Getenv(k) == "" {
			fmt.Println("diagnostic configuration missing")
			os.Exit(1)
		}
	}
	c, e := openapi.NewClient(&openapi.Config{AccessKeyId: dara.String(os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")), AccessKeySecret: dara.String(os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")), SecurityToken: dara.String(os.Getenv("ALIBABA_CLOUD_SECURITY_TOKEN")), Type: dara.String("sts"), Endpoint: dara.String("nlb.ap-southeast-1.aliyuncs.com"), Protocol: dara.String("HTTPS")})
	if e != nil {
		os.Exit(1)
	}
	rows := []map[string]interface{}{}
	for kind, id := range map[string]string{"servergroup": "sgp-xs5s42tihtl6ja2a67", "loadbalancer": "nlb-jqbxi42nurq0qkb7e9"} {
		v, err := probe(c, kind, id)
		row := map[string]interface{}{"resourceType": kind, "resourceId": id, "response": v}
		if err != nil {
			row["error"] = err
			row["errorText"] = err.Error()
		}
		rows = append(rows, row)
	}
	raw, e := json.Marshal(rows)
	if e != nil || len(raw) > 262144 {
		os.Exit(1)
	}
	if os.WriteFile(os.Getenv("PROBE_PRIVATE_OUTPUT"), raw, 0600) != nil {
		os.Exit(1)
	}
	fmt.Println("Two permission-only probes captured privately; no tag mutation requested.")
}
