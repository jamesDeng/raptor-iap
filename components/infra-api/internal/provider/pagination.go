package provider

import (
	"context"
	"errors"
	"raptor-iap/infra-api/internal/domain"
)

type record struct {
	ID, Name, State, Endpoint string
	Tags                      map[string]string
}
type page struct {
	Records []record
	Total   int
}

func (p *reader) discover(ctx context.Context, env domain.Environment, kind, code string) ([]record, error) {
	if p.page == nil {
		return nil, domain.ErrNotConfigured
	}
	all := []record{}
	seen := map[string]bool{}
	total := -1
	for n := 1; n <= 100; n++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pg, e := p.page(ctx, env, kind, code, n)
		if e != nil {
			return nil, e
		}
		if pg.Total < 0 || pg.Total > 10000 || (total >= 0 && total != pg.Total) {
			return nil, errors.New("incomplete discovery")
		}
		total = pg.Total
		for _, r := range pg.Records {
			if r.ID == "" || seen[r.ID] {
				return nil, errors.New("incomplete discovery")
			}
			seen[r.ID] = true
			all = append(all, r)
		}
		if len(all) == total {
			return all, nil
		}
		if len(all) > total || len(pg.Records) == 0 {
			return nil, errors.New("incomplete discovery")
		}
	}
	return nil, errors.New("pagination exhausted")
}
func project(r record, env domain.Environment, kind, code, evidence string) domain.Deployment {
	return domain.Deployment{ResourceID: r.ID, Kind: kind, EnvCode: env.Code, ObjectCode: code, Name: r.Name, State: r.State, Endpoint: r.Endpoint, TargetDBCode: r.Tags["target-db-code"], EvidenceMode: evidence}
}

// tokenPages enforces complete stable totals and unique records/tokens for NLB.
type tokenPages struct {
	total       int
	token       string
	tokens, ids map[string]bool
}

func newTokenPages() *tokenPages {
	return &tokenPages{total: -1, tokens: map[string]bool{}, ids: map[string]bool{}}
}
func (p *tokenPages) add(id string) bool {
	if id == "" || p.ids[id] {
		return false
	}
	p.ids[id] = true
	return true
}
func (p *tokenPages) next(total *int32, token string, count int) (bool, error) {
	if total == nil || *total < 0 || *total > 10000 || (p.total >= 0 && p.total != int(*total)) {
		return false, providerRefusal()
	}
	p.total = int(*total)
	if len(p.ids) > p.total {
		return false, providerRefusal()
	}
	if token == "" {
		if len(p.ids) != p.total {
			return false, providerRefusal()
		}
		return true, nil
	}
	if count == 0 || p.tokens[token] || len(p.ids) >= p.total {
		return false, providerRefusal()
	}
	p.tokens[token] = true
	p.token = token
	return false, nil
}
