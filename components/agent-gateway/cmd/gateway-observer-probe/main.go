package main

import (
	"context"
	"encoding/json"
	runtime "github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/runtime"
	"io"
	"os"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var input runtime.ObserverProbeInput
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 131073))
	decoder.DisallowUnknownFields()
	var extra any
	if decoder.Decode(&input) != nil || decoder.Decode(&extra) != io.EOF {
		os.Exit(1)
	}
	raw, e := runtime.ExecuteObserverProbe(ctx, input)
	if e != nil || len(raw) > 262144 {
		os.Exit(1)
	}
	os.Stdout.Write(raw)
}
