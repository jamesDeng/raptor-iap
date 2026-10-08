package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/evidence"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	targetsFile := flag.String("targets-file", "", "operator-provided JSON pod UID / endpoint roster")
	duration := flag.Duration("duration", 5*time.Minute, "bounded capture duration")
	interval := flag.Duration("interval", 5*time.Second, "snapshot poll interval")
	flag.Parse()
	f, e := os.Open(*targetsFile)
	if e != nil {
		fmt.Fprintln(os.Stderr, "target roster required")
		os.Exit(2)
	}
	var targets []evidence.Target
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if d.Decode(&targets) != nil {
		fmt.Fprintln(os.Stderr, "invalid roster")
		os.Exit(2)
	}
	_ = f.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	b, e := evidence.Capture(ctx, targets, *duration, *interval)
	if e != nil {
		fmt.Fprintln(os.Stderr, "invalid capture configuration")
		os.Exit(2)
	}
	_ = json.NewEncoder(os.Stdout).Encode(b)
	if b.Summary(15*time.Second).Status != "pass" {
		os.Exit(1)
	}
}
