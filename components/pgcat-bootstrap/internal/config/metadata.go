package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"
)

const MaxBundle = 256 * 1024

var codePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

type Reference struct{ Bucket, Key, Version string }
type Metadata struct {
	Env             string    `json:"env"`
	ProxyCode       string    `json:"proxy_code"`
	TargetDBCode    string    `json:"target_db_code"`
	DBHost          string    `json:"db_host"`
	Database        string    `json:"database"`
	SecretReference string    `json:"secret_reference"`
	Revision        string    `json:"revision"`
	Reference       Reference `json:"-"`
}

// Reject duplicate keys recursively: encoding/json otherwise silently takes the last.
func unique(d *json.Decoder) error {
	token, e := d.Token()
	if e != nil {
		return e
	}
	switch token {
	case json.Delim('{'):
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
			if e := unique(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	case json.Delim('['):
		for d.More() {
			if e := unique(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	}
	return nil
}
func decode(data []byte, dst any) error {
	if len(data) == 0 || len(data) > MaxBundle {
		return errors.New("invalid_json")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if unique(d) != nil {
		return errors.New("invalid_json")
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("invalid_json")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		return errors.New("invalid_json")
	}
	return nil
}
func ParseMetadata(data []byte, bakedRevision string) (Metadata, error) {
	var m Metadata
	if e := decode(data, &m); e != nil {
		return m, e
	}
	if !revisionPattern.MatchString(bakedRevision) || m.Revision != bakedRevision {
		return Metadata{}, errors.New("revision_mismatch")
	}
	for _, s := range []string{m.Env, m.ProxyCode, m.TargetDBCode, m.DBHost, m.Database} {
		if !codePattern.MatchString(s) {
			return Metadata{}, errors.New("invalid_identity")
		}
	}
	u, e := url.Parse(m.SecretReference)
	if e != nil || u.Scheme != "oss" || u.User != nil || u.Fragment != "" || !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`).MatchString(u.Host) {
		return Metadata{}, errors.New("invalid_reference")
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil || len(q) != 1 || len(q["versionId"]) != 1 || q.Get("versionId") == "" || len(q.Get("versionId")) > 1024 {
		return Metadata{}, errors.New("invalid_reference")
	}
	key := strings.TrimPrefix(u.Path, "/")
	if key == "" || len(key) > 1024 || strings.ContainsAny(key, "\x00\r\n") || strings.Contains(key, "..") {
		return Metadata{}, errors.New("invalid_reference")
	}
	m.Reference = Reference{u.Host, key, q.Get("versionId")}
	return m, nil
}
