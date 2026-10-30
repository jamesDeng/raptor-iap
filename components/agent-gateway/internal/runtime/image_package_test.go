package runtime

import (
	"os"
	"strings"
	"testing"
)

func TestGatewayImageIncludesEveryDeliveredHarnessModule(t *testing.T) {
	raw, e := os.ReadFile("../../Dockerfile")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range harnessFiles {
		if !strings.Contains(string(raw), "/"+name+" ") {
			t.Errorf("Gateway image missing harness module: %s", name)
		}
	}
}
func TestGatewayImageIncludesObservationExecutor(t *testing.T) {
	raw, e := os.ReadFile("../../Dockerfile")
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range []string{"./cmd/gateway-observer-probe", "nodejs", "/gateway-observation-runner.mjs "} {
		if !strings.Contains(string(raw), item) {
			t.Errorf("missing observation runtime: %s", item)
		}
	}
}

func TestGatewayImageSmokeRunsObservationExecutor(t *testing.T) {
	raw, e := os.ReadFile("../../../../scripts/rdev/check-platform-image.sh")
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range []string{"gateway-observer-probe", "import(\"/opt/raptor-harness/gateway-observation-runner.mjs\")", "InvalidObservation"} {
		if !strings.Contains(string(raw), item) {
			t.Errorf("missing image observation smoke: %s", item)
		}
	}
}

func TestGatewayImagePackagesLockedMCPRuntime(t *testing.T) {
	raw, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []string{"npm ci --omit=dev --ignore-scripts", "/node_modules/@earendil-works/pi-mcp", "/node_modules/cross-spawn", "/node_modules/path-key", "/node_modules/shebang-command", "/node_modules/shebang-regex", "/node_modules/which", "/node_modules/isexe"} {
		if !strings.Contains(string(raw), item) {
			t.Errorf("missing existing locked MCP runtime: %s", item)
		}
	}
}
