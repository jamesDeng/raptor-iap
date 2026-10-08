package runtime

import (
	"testing"
)

func TestAXEndpointRequiresPrivateHTTPS(t *testing.T) {
	for _, v := range []string{"http://10.70.1.180:8443", "https://43.106.61.98:8443", "https://10.70.1.180:8443/?x=1", "https://user:pass@10.70.1.180:8443"} {
		if validateAXEndpoint(v) == nil {
			t.Fatalf("unsafe endpoint %s accepted", v)
		}
	}
	if validateAXEndpoint("https://10.70.1.180:8443") != nil {
		t.Fatal("private endpoint rejected")
	}
}
