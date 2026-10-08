package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataInputBound(t *testing.T) {
	p := filepath.Join(t.TempDir(), "metadata")
	if e := os.WriteFile(p, make([]byte, 16385), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := readMetadata(p); e == nil {
		t.Fatal("oversized metadata accepted")
	}
}
