package jsoninput

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
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
	var value any
	if e := json.Unmarshal(raw, &value); e != nil {
		return e
	}
	if e := exactFields(value, reflect.TypeOf(out)); e != nil {
		return e
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(out)
}

func exactFields(value any, t reflect.Type) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return errors.New("expected object")
		}
		fields := map[string]reflect.Type{}
		var collect func(reflect.Type)
		collect = func(current reflect.Type) {
			for i := 0; i < current.NumField(); i++ {
				f := current.Field(i)
				if f.Anonymous {
					collect(f.Type)
					continue
				}
				name := strings.Split(f.Tag.Get("json"), ",")[0]
				if name == "-" {
					continue
				}
				if name == "" {
					name = f.Name
				}
				fields[name] = f.Type
			}
		}
		collect(t)
		for key, v := range object {
			field, ok := fields[key]
			if !ok {
				return errors.New("unknown field")
			}
			if e := exactFields(v, field); e != nil {
				return e
			}
		}
	case reflect.Slice:
		if value == nil {
			return nil
		}
		items, ok := value.([]any)
		if !ok {
			return errors.New("expected array")
		}
		for _, v := range items {
			if e := exactFields(v, t.Elem()); e != nil {
				return e
			}
		}
	}
	return nil
}
