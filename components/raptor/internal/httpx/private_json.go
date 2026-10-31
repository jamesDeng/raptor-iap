package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// DecodePrivate rejects ambiguous JSON on the fresh-proof service boundary.
// Public legacy DTOs keep their existing decoder. Private messages have no nulls,
// duplicate keys, case-folded aliases, unknown keys or trailing values.
func DecodePrivate(w http.ResponseWriter, r *http.Request, out any) bool {
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
	fail := func() bool { Error(w, 400, "InvalidInput"); return false }
	if e != nil {
		return fail()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		tok, e := d.Token()
		if e != nil {
			return e
		}
		if tok == nil {
			return io.ErrUnexpectedEOF
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, e := d.Token()
					if e != nil {
						return e
					}
					k, ok := key.(string)
					if !ok || seen[k] {
						return io.ErrUnexpectedEOF
					}
					seen[k] = true
					if e = walk(); e != nil {
						return e
					}
				}
			case '[':
				for d.More() {
					if e := walk(); e != nil {
						return e
					}
				}
			default:
				return io.ErrUnexpectedEOF
			}
			_, e = d.Token()
			return e
		}
		return nil
	}
	if walk() != nil {
		return fail()
	}
	if _, e = d.Token(); e != io.EOF {
		return fail()
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return fail()
	}
	// encoding/json accepts case-insensitive struct fields. Require every supplied
	// key to survive canonical re-encoding with exactly its original spelling.
	canonical, e := json.Marshal(out)
	if e != nil {
		return fail()
	}
	var supplied, decoded any
	if json.Unmarshal(raw, &supplied) != nil || json.Unmarshal(canonical, &decoded) != nil {
		return fail()
	}
	var keys func(any, any) bool
	keys = func(a, b any) bool {
		switch x := a.(type) {
		case map[string]any:
			y, ok := b.(map[string]any)
			if !ok {
				return false
			}
			for k, v := range x {
				other, ok := y[k]
				if !ok || !keys(v, other) {
					return false
				}
			}
		case []any:
			y, ok := b.([]any)
			if !ok || len(x) != len(y) {
				return false
			}
			for i, v := range x {
				if !keys(v, y[i]) {
					return false
				}
			}
		}
		return true
	}
	if !keys(supplied, decoded) {
		return fail()
	}
	return true
}
