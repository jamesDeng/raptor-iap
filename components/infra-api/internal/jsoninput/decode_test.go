package jsoninput

import "testing"

func TestDecodeRejectsFieldAliases(t *testing.T) {
	for _, raw := range []string{`{"Protected":true}`, `{"protected":true,"Protected":false}`} {
		var out struct {
			Protected bool `json:"protected"`
		}
		if Decode([]byte(raw), &out) == nil {
			t.Fatalf("alias accepted: %s", raw)
		}
	}
}
