package main

import (
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestPrivatePOCPlaintextIsExplicit(t *testing.T) {
	c, e := pgx.ParseConfig("postgres://app:synthetic@10.0.0.1/test?sslmode=disable")
	if e != nil {
		t.Fatal(e)
	}
	if validateTransport(c, false) == nil {
		t.Fatal("plaintext accepted without opt-in")
	}
	if e := validateTransport(c, true); e != nil {
		t.Fatal(e)
	}
}
