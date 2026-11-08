package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash/crc64"
	"io"
	"os"
	"regexp"
	"strconv"
	"time"
)

const (
	accountID       = "1360282071200743"
	region          = "ap-southeast-1"
	functionName    = "raptor-rdev-infra-read"
	functionID      = "e3c7abb1-dae4-4ba5-a0df-4ba516090ba4"
	functionRuntime = "custom.debian10"
	maxPackageSize  = 32 << 20
)

type functionState struct {
	ID           string
	Name         string
	Runtime      string
	Checksum     string
	UpdateStatus string
}

func (s functionState) valid() bool {
	return s.ID == functionID && s.Name == functionName && s.Runtime == functionRuntime && s.Checksum != ""
}

func (s functionState) ready() bool {
	return s.UpdateStatus == "" || s.UpdateStatus == "Successful"
}

type functionAPI interface {
	read(context.Context) (functionState, error)
	update(context.Context, []byte) error
}

func deploy(ctx context.Context, api functionAPI, previous, target string, packageBytes []byte, interval time.Duration) (string, error) {
	state, err := api.read(ctx)
	if err != nil || !state.valid() || !state.ready() {
		return "", errors.New("function identity or readiness could not be verified")
	}
	if state.Checksum == target {
		return "already-deployed", nil
	}
	if state.Checksum != previous {
		return "", errors.New("current function checksum differs from the reviewed predecessor")
	}

	// A lost response is reconciled by reading the function. Do not replay the update.
	_ = api.update(ctx, packageBytes)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		state, err = api.read(ctx)
		if err == nil {
			if !state.valid() || (state.Checksum != previous && state.Checksum != target) {
				return "", errors.New("function identity or checksum changed during release")
			}
			if state.UpdateStatus == "Failed" {
				return "", errors.New("provider reported function update failure")
			}
			if state.Checksum == target && state.ready() {
				return "verified", nil
			}
		}
		select {
		case <-ctx.Done():
			return "", errors.New("function update outcome is unconfirmed")
		case <-ticker.C:
		}
	}
}

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func readPackage(path, digest string) ([]byte, string, error) {
	if !digestPattern.MatchString(digest) {
		return nil, "", errors.New("invalid package digest")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", errors.New("cannot open reviewed package")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPackageSize+1))
	if err != nil || len(data) == 0 || len(data) > maxPackageSize || fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
		return nil, "", errors.New("reviewed package digest or size mismatch")
	}
	return data, strconv.FormatUint(crc64.Checksum(data, crc64.MakeTable(crc64.ECMA)), 10), nil
}

func run(args []string) error {
	if len(args) != 3 {
		return errors.New("expected package path, SHA-256 and predecessor checksum")
	}
	if previous, err := strconv.ParseUint(args[2], 10, 64); err != nil || previous == 0 {
		return errors.New("invalid predecessor checksum")
	}
	data, checksum, err := readPackage(args[0], args[1])
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	api, err := newFunctionAPI(ctx)
	if err != nil {
		return err
	}
	result, err := deploy(ctx, api, args[2], checksum, data, 3*time.Second)
	if err != nil {
		return err
	}
	fmt.Printf("Infra API %s at checksum %s\n", result, checksum)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
