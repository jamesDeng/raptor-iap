package requests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	operations "github.com/jamesDeng/raptor-iap/components/raptor/operation-schemas"
	"math"
	"reflect"
	"strings"
	"unicode/utf8"
)

type Field struct {
	Type      string   `json:"type"`
	Minimum   *float64 `json:"minimum,omitempty"`
	MinLength int      `json:"minLength,omitempty"`
	MaxLength int      `json:"maxLength,omitempty"`
	MaxBytes  int      `json:"maxBytes,omitempty"`
	Multiline bool     `json:"x-multiline,omitempty"`
	Enum      []any    `json:"enum,omitempty"`
	Default   any      `json:"default,omitempty"`
}
type Schema struct {
	Name                 string           `json:"x-operation"`
	ObjectKind           string           `json:"x-object-kind"`
	Type                 string           `json:"type"`
	Required             []string         `json:"required"`
	Properties           map[string]Field `json:"properties"`
	AdditionalProperties bool             `json:"additionalProperties"`
}

func Schemas() map[string]Schema {
	entries, e := operations.Files.ReadDir(".")
	if e != nil {
		panic(e)
	}
	out := map[string]Schema{}
	for _, f := range entries {
		b, e := operations.Files.ReadFile(f.Name())
		if e != nil {
			panic(e)
		}
		var schema Schema
		if e = json.Unmarshal(b, &schema); e != nil {
			panic(e)
		}
		out[schema.Name] = schema
	}
	return out
}
func schemaHash() string {
	b, _ := json.Marshal(Schemas())
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (s *Service) Validate(in domain.RequestInput) error {
	if in.Type == "direct" {
		if in.Operation != "application.restart" || len(in.Targets) == 0 || len(in.Targets) > 50 || len(in.Operations) > 0 || in.Object.Code != "" || in.EnvCode != "" {
			return domain.ErrInvalid
		}
		seen := map[string]bool{}
		for _, t := range in.Targets {
			if t.AppCode == "" || t.EnvCode == "" || t.ClusterID == "" || t.Namespace == "" || t.Name == "" || t.UID == "" {
				return domain.ErrInvalid
			}
			key := TargetKey(t)
			if seen[key] {
				return domain.ErrInvalid
			}
			seen[key] = true
		}
		return nil
	}
	if in.Type != "agent" || in.Object.Code == "" || in.Object.Kind == "" || in.EnvCode == "" || len(in.Operations) == 0 || len(in.Operations) > 8 || len(in.Targets) > 0 || in.Operation != "" {
		return domain.ErrInvalid
	}
	for _, op := range in.Operations {
		if op.Name == "application.question" && (len(in.Operations) != 1 || (s.ModelPolicy == nil && in.Model != "gpt-5.6-luna") || (s.ModelPolicy != nil && (in.ProviderID != "codex" || in.Model == ""))) {
			return domain.ErrInvalid
		}
	}
	schemas := Schemas()
	for _, op := range in.Operations {
		def, ok := schemas[op.Name]
		if !ok || def.ObjectKind != in.Object.Kind || op.Parameters == nil {
			return domain.ErrInvalid
		}
		for _, key := range def.Required {
			if _, ok = op.Parameters[key]; !ok {
				return domain.ErrInvalid
			}
		}
		for k, v := range op.Parameters {
			field, ok := def.Properties[k]
			if !ok {
				return domain.ErrInvalid
			}
			switch field.Type {
			case "string":
				x, ok := v.(string)
				maxBytes := field.MaxBytes
				if maxBytes == 0 {
					maxBytes = 200
				}
				if !ok || !utf8.ValidString(x) || len(x) > maxBytes || utf8.RuneCountInString(x) < field.MinLength || (field.MaxLength > 0 && utf8.RuneCountInString(x) > field.MaxLength) || (field.Multiline && strings.TrimSpace(x) == "") {
					return domain.ErrInvalid
				}
			case "integer":
				var n float64
				switch x := v.(type) {
				case float64:
					n = x
				case int:
					n = float64(x)
				case json.Number:
					var e error
					n, e = x.Float64()
					if e != nil {
						return domain.ErrInvalid
					}
				default:
					return domain.ErrInvalid
				}
				if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) || n > 9007199254740991 || (field.Minimum != nil && n < *field.Minimum) {
					return domain.ErrInvalid
				}
			default:
				return domain.ErrInvalid
			}
			if len(field.Enum) > 0 {
				match := false
				for _, allowed := range field.Enum {
					if reflect.DeepEqual(v, allowed) {
						match = true
					}
				}
				if !match {
					return domain.ErrInvalid
				}
			}
		}
	}
	return nil
}
func applyDefaults(in *domain.RequestInput) {
	for i := range in.Operations {
		def := Schemas()[in.Operations[i].Name]
		for k, f := range def.Properties {
			if _, ok := in.Operations[i].Parameters[k]; !ok && f.Default != nil {
				in.Operations[i].Parameters[k] = f.Default
			}
		}
	}
}
func TargetKey(t domain.RestartTarget) string {
	b, _ := json.Marshal(t)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
