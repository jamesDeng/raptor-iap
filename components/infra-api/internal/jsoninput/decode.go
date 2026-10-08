package jsoninput

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Reject duplicates recursively before decoding: encoding/json otherwise keeps
// the final value, including duplicates inside node arrays/embedded objects.
func Decode(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > 8192 {
		return errors.New("invalid input")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		tok, e := d.Token()
		if e != nil {
			return e
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				s, ok := k.(string)
				if !ok || seen[s] {
					return errors.New("duplicate key")
				}
				seen[s] = true
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
			return errors.New("invalid input")
		}
		_, e = d.Token()
		return e
	}
	if e := walk(); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing input")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
