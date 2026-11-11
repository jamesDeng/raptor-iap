package runtime

import "testing"

func TestFleetObservationDiagnosticDropsUntrustedFields(t *testing.T) {
	const id = "01A23BCD-4567-89AB-CDEF-0123456789AB"
	d, ok := parseFleetObservationDiagnostic([]byte(`{
		"stage":"mcp_call","category":"upstream_tool_error","upstreamId":"raw-fixture-secret","httpStatus":200,
		"observationId":"` + id + `","elapsedMs":25,
		"infraFailure":{"stage":"ess_scaling_instances","category":"throttled","upstreamId":"` + id + `","httpStatus":429,"elapsedMs":2,"raw":"private"}
	}`))
	if !ok || d.UpstreamID != "" || d.ObservationID != id || d.InfraFailure == nil || d.InfraFailure.UpstreamID != id || d.InfraFailure.HTTPStatus != 429 {
		t.Fatalf("unexpected sanitized diagnostic: %+v", d)
	}

	d, ok = parseFleetObservationDiagnostic([]byte(`{
		"stage":"mcp_call","category":"upstream_tool_error","elapsedMs":25,
		"infraFailure":{"stage":"ess_scaling_instances","category":"raw-fixture-secret","elapsedMs":2}
	}`))
	if !ok || d.InfraFailure != nil {
		t.Fatal("unexpected category was retained")
	}
}
