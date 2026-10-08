// evidence evaluates a bounded, externally collected bundle. It never queries cloud APIs.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/evidence"
	"io"
	"os"
	"time"
)

func main() {
	raw, e := io.ReadAll(io.LimitReader(os.Stdin, (16<<20)+1))
	if e != nil || len(raw) > 16<<20 {
		fmt.Fprintln(os.Stderr, "oversized evidence bundle")
		os.Exit(2)
	}
	var b evidence.Bundle
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&b) != nil {
		fmt.Fprintln(os.Stderr, "invalid evidence bundle")
		os.Exit(2)
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fmt.Fprintln(os.Stderr, "trailing input")
		os.Exit(2)
	}
	s := b.Summary(15 * time.Second)
	_ = json.NewEncoder(os.Stdout).Encode(s)
	if s.Status != "pass" {
		os.Exit(1)
	}
}
