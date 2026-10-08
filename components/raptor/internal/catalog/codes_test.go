package catalog

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestObjectCodesIndependentAndNeverReused(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	for _, v := range []struct{ kind, want string }{{"application", "A00001"}, {"database", "D00001"}, {"db-proxy", "DP00001"}, {"application", "A00002"}} {
		o, e := s.CreateObject(ctx, CreateObjectInput{Kind: v.kind, Name: v.kind})
		if e != nil {
			t.Fatal(e)
		}
		if o.Code != v.want {
			t.Fatalf("got %q want %q", o.Code, v.want)
		}
	}
	if _, e := s.Pool.Exec(ctx, "DELETE FROM raptor.objects WHERE code='A00002'"); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateObject(ctx, CreateObjectInput{Kind: "application", Name: "next"})
	if e != nil || o.Code != "A00003" {
		t.Fatalf("code reused: %+v %v", o, e)
	}
}
func TestObjectCodesConcurrent(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	codes := make(chan string, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, e := s.CreateObject(ctx, CreateObjectInput{Kind: "application", Name: "parallel"})
			if e != nil {
				t.Error(e)
				return
			}
			codes <- o.Code
		}()
	}
	wg.Wait()
	close(codes)
	seen := map[string]bool{}
	for c := range codes {
		if seen[c] {
			t.Fatal("duplicate", c)
		}
		seen[c] = true
	}
	for i := 1; i <= 24; i++ {
		if !seen[fmt.Sprintf("A%05d", i)] {
			t.Fatalf("missing sequence %d: %v", i, seen)
		}
	}
}
func TestObjectCodePaddingExpands(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	if _, e := s.Pool.Exec(ctx, "SELECT setval('raptor.application_code_seq',99999,true)"); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateObject(ctx, CreateObjectInput{Kind: "application", Name: "overflow"})
	if e != nil || o.Code != "A100000" {
		t.Fatalf("overflow: %+v %v", o, e)
	}
}

func TestLegacyAliasFindsSameObjectOnlyWithinKind(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	obj, e := s.CreateObject(ctx, CreateObjectInput{Kind: "application", Name: "Migrated"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "INSERT INTO raptor.object_code_aliases(code,object_id,kind,canonical_code) VALUES('old-app',$1,'application',$2)", obj.ID, obj.Code); e != nil {
		t.Fatal(e)
	}
	got, e := s.FindObject(ctx, "application", "old-app")
	if e != nil || got.ID != obj.ID || got.Code != obj.Code {
		t.Fatalf("alias lost object identity: %+v %v", got, e)
	}
	if _, e = s.FindObject(ctx, "database", "old-app"); e == nil {
		t.Fatal("alias crossed object kinds")
	}
}
