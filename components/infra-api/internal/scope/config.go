package scope

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"raptor-iap/infra-api/internal/domain"
	"strings"
)

func Load(path string) (map[string]domain.Environment, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("scope unavailable")
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	var rows []domain.Environment
	if e = d.Decode(&rows); e != nil {
		return nil, errors.New("invalid scope")
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid scope")
	}
	out := map[string]domain.Environment{}
	for _, r := range rows {
		if strings.TrimSpace(r.Code) == "" || len(r.Code) > 256 || strings.TrimSpace(r.AccountID) == "" || r.Region != "ap-southeast-1" {
			return nil, errors.New("invalid scope")
		}
		if _, ok := out[r.Code]; ok {
			return nil, errors.New("duplicate scope")
		}
		codes, groups, backends := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for _, p := range r.Proxies {
			for _, value := range []string{p.Code, p.GroupID, p.ServerGroupID, p.ListenerID, p.TargetDBCode} {
				if strings.TrimSpace(value) != value || value == "" || len(value) > 256 {
					return nil, errors.New("invalid proxy mapping")
				}
			}
			if p.Port < 1 || p.Port > 65535 || codes[p.Code] || groups[p.GroupID] || backends[p.ServerGroupID] {
				return nil, errors.New("invalid proxy mapping")
			}
			codes[p.Code], groups[p.GroupID], backends[p.ServerGroupID] = true, true, true
		}
		out[r.Code] = r
	}
	if len(out) == 0 {
		return nil, errors.New("empty scope")
	}
	return out, nil
}
