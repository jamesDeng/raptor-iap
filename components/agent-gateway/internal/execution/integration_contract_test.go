package execution

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func producerFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile("../../../../scripts/contracts/live-question/fixtures/" + name)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func producerInput(t *testing.T) ExecutionInput {
	t.Helper()
	var in ExecutionInput
	if e := json.Unmarshal(producerFixture(t, "raptor-context.json"), &in); e != nil {
		t.Fatal(e)
	}
	return in
}
func TestParseRaptorQuestionDefinition(t *testing.T) {
	in := producerInput(t)
	var ctx map[string]any
	json.Unmarshal(producerFixture(t, "raptor-context.json"), &ctx)
	b, q, h, e := ParseLiveQuestion(in, "22222222-2222-4222-8222-222222222222")
	if e != nil || q != "Is gateway healthy?" || b.Model != "gpt-5.6-luna" || h != ctx["definitionSha256"] {
		t.Fatalf("producer definition rejected: %v", e)
	}
}
func TestCanonicalDefinitionFingerprint(t *testing.T) {
	in := producerInput(t)
	var ctx map[string]any
	json.Unmarshal(producerFixture(t, "raptor-context.json"), &ctx)
	var d map[string]any
	json.Unmarshal(in.Definition, &d)
	in.Definition, _ = json.Marshal(d)
	_, _, h, e := ParseLiveQuestion(in, "22222222-2222-4222-8222-222222222222")
	if e != nil || h != ctx["definitionSha256"] {
		t.Fatal("canonical hash differs", e)
	}
}
func TestIssueAcceptsCreatedAndRejectsForeignBinding(t *testing.T) {
	raw := producerFixture(t, "raptor-access.json")
	var env struct {
		Data struct {
			Binding   AttemptBinding
			ExpiresAt time.Time
		}
	}
	json.Unmarshal(raw, &env)
	var ctx map[string]any
	json.Unmarshal(producerFixture(t, "raptor-context.json"), &ctx)
	mode := "valid"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.Unmarshal(raw, &body)
		data := body["data"].(map[string]any)
		binding := data["binding"].(map[string]any)
		switch mode {
		case "hash":
			binding["definitionSha256"] = "wrong"
		case "cluster":
			binding["clusterId"] = "other"
		case "attempt":
			binding["attemptId"] = "other"
		case "expiry":
			data["expiresAt"] = "2000-01-01T00:00:00Z"
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(body)
	}))
	defer s.Close()
	c := HTTPRaptor{BaseURL: s.URL, Username: "service", Password: "fixture"}
	if _, e := c.Issue(context.Background(), env.Data.Binding, ctx["definitionSha256"].(string), env.Data.ExpiresAt); e != nil {
		t.Fatal("valid Created response rejected", e)
	}
	for _, mode = range []string{"hash", "cluster", "attempt", "expiry"} {
		if _, e := c.Issue(context.Background(), env.Data.Binding, ctx["definitionSha256"].(string), env.Data.ExpiresAt); e == nil {
			t.Fatal("foreign access accepted", mode)
		}
	}
}

func TestCanonicalQuestionRejectsConflictingContext(t *testing.T) {
	for _, which := range []string{"none", "two", "model", "skills", "hash", "request", "environment"} {
		t.Run(which, func(t *testing.T) {
			in := producerInput(t)
			var d map[string]any
			json.Unmarshal(in.Definition, &d)
			switch which {
			case "none":
				d["operations"] = []any{}
			case "two":
				d["operations"] = append(d["operations"].([]any), d["operations"].([]any)[0])
			case "model":
				d["model"] = "other"
			case "skills":
				in.Skills.CommitSHA = "other"
			case "hash":
				in.DefinitionSHA256 = "bad"
			case "request":
				in.RequestID = ""
			case "environment":
				in.Environment = json.RawMessage(`{}`)
			}
			in.Definition, _ = json.Marshal(d)
			if _, _, _, e := ParseLiveQuestion(in, "attempt"); e == nil {
				t.Fatal("conflicting context accepted")
			}
		})
	}
}

func TestLiveResultEvidenceBound(t *testing.T) {
	var v struct{ Result LiveResult }
	json.Unmarshal(producerFixture(t, "gateway-execution.json"), &v)
	var access struct {
		Data struct{ Binding AttemptBinding }
	}
	json.Unmarshal(producerFixture(t, "raptor-access.json"), &access)
	for len(v.Result.Evidence) < 33 {
		v.Result.Evidence = append(v.Result.Evidence, v.Result.Evidence[0])
	}
	if ValidateLiveResult(v.Result, access.Data.Binding) == nil {
		t.Fatal("33 evidence entries accepted")
	}
}

func TestLiveResultRejectsBadUsageAndConfiguredCluster(t *testing.T) {
	for _, which := range []string{"usage", "cluster", "oversized"} {
		t.Run(which, func(t *testing.T) {
			var v struct{ Result LiveResult }
			json.Unmarshal(producerFixture(t, "gateway-execution.json"), &v)
			var access struct {
				Data struct{ Binding AttemptBinding }
			}
			json.Unmarshal(producerFixture(t, "raptor-access.json"), &access)
			if which == "oversized" {
				v.Result.Usage[0].Input = 9007199254740992
			} else if which == "usage" {
				v.Result.Usage[0].Input = -1
			} else {
				for i := range v.Result.Evidence {
					if v.Result.Evidence[i].Identity.ClusterID != "" {
						v.Result.Evidence[i].Identity.ClusterID = "foreign"
					}
				}
			}
			if ValidateLiveResult(v.Result, access.Data.Binding) == nil {
				t.Fatal("invalid usage/configured cluster accepted")
			}
		})
	}
}
