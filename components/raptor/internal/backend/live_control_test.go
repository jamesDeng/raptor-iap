package backend

import (
	"encoding/json"
	"testing"
)

func TestServiceReadsCurrentRequestControl(t *testing.T) {
	s, r, _ := accessFixture(t)
	w := accessCall(s, "GET", "/v1/requests/"+r.ID+"/control", "fixture-controller", "fixture-controller-password", nil)
	if w.Code != 200 {
		t.Fatalf("control endpoint unavailable: %d", w.Code)
	}
	var out struct {
		Data struct {
			RequestID    string `json:"requestId"`
			ControlState string `json:"controlState"`
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.RequestID != r.ID || out.Data.ControlState != "" {
		t.Fatal("incorrect control snapshot")
	}
}
