package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fixtureAPI struct {
	rows      []functionState
	calls     int
	updated   []byte
	submitErr error
}

func (f *fixtureAPI) read(context.Context) (functionState, error) {
	if len(f.rows) == 0 {
		return functionState{}, errors.New("no provider response")
	}
	state := f.rows[0]
	f.rows = f.rows[1:]
	return state, nil
}

func (f *fixtureAPI) update(_ context.Context, packageBytes []byte) error {
	f.calls++
	f.updated = packageBytes
	return f.submitErr
}

func TestReleaseAdmissionAndReconciliation(t *testing.T) {
	prior := functionState{ID: functionID, Name: functionName, Runtime: functionRuntime, Checksum: "previous"}
	current := prior
	current.Checksum = "new"
	foreign := prior
	foreign.ID = "other"
	busy := prior
	busy.UpdateStatus = "InProgress"
	failed := current
	failed.UpdateStatus = "Failed"
	for _, tc := range []struct {
		name      string
		states    []functionState
		submitErr error
		calls     int
		wantErr   bool
	}{
		{"idempotent", []functionState{current}, nil, 0, false},
		{"foreign", []functionState{foreign}, nil, 0, true},
		{"wrong previous", []functionState{prior}, nil, 0, true},
		{"busy", []functionState{busy}, nil, 0, true},
		{"submitted", []functionState{prior, current}, nil, 1, false},
		{"uncertain response reconciled", []functionState{prior, current}, errors.New("transport lost"), 1, false},
		{"failed update", []functionState{prior, failed}, nil, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fixtureAPI{rows: tc.states, submitErr: tc.submitErr}
			expected := "previous"
			if tc.name == "wrong previous" {
				expected = "different"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err := deploy(ctx, f, expected, "new", []byte("package"), time.Millisecond)
			if (err != nil) != tc.wantErr || f.calls != tc.calls {
				t.Fatalf("err=%v calls=%d", err, f.calls)
			}
		})
	}
}
