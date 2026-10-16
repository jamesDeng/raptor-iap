package control

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

const SocketPath = "/run/traffic-control/finish.sock"
const requestTimeout = 2 * time.Second

type request struct {
	PodUID    string `json:"podUID"`
	ProcessID string `json:"processID"`
}

func Serve(ctx context.Context, path string, c *Controller) error {
	if c == nil {
		return fmt.Errorf("control unavailable")
	}
	// Never unlink a pre-existing socket: ambiguous ownership fails closed.
	l, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("control listen failed")
	}
	defer l.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("control permissions failed")
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			l.Close()
		case <-stop:
		}
	}()
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("control accept failed")
		}
		// Serial requests bound resource use and each request has a finite deadline.
		conn.SetDeadline(time.Now().Add(requestTimeout))
		var r request
		line, readErr := bufio.NewReader(io.LimitReader(conn, 4097)).ReadBytes('\n')
		d := json.NewDecoder(bytes.NewReader(line))
		d.DisallowUnknownFields()
		status := Status("")
		if readErr == nil && len(line) <= 4096 && d.Decode(&r) == nil && d.Decode(new(any)) == io.EOF {
			status, _ = c.finish(r.PodUID, r.ProcessID)
		}
		_ = json.NewEncoder(conn).Encode(status)
		conn.Close()
	}
}
func Request(ctx context.Context, path, uid, pid string) (Status, error) {
	fail := func() (Status, error) {
		return "", fmt.Errorf("finish acknowledgement unavailable; reconcile exact pod identity and evidence")
	}
	if uid == "" || pid == "" {
		return fail()
	}
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0600 {
		return fail()
	}
	bounded, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(bounded, "unix", path)
	if err != nil {
		return fail()
	}
	defer conn.Close()
	deadline, _ := bounded.Deadline()
	conn.SetDeadline(deadline)
	stop := context.AfterFunc(bounded, func() { conn.Close() })
	defer stop()
	if json.NewEncoder(conn).Encode(request{uid, pid}) != nil {
		return fail()
	}
	var status Status
	line, readErr := bufio.NewReader(io.LimitReader(conn, 4097)).ReadBytes('\n')
	if readErr != nil || len(line) > 4096 || json.Unmarshal(line, &status) != nil {
		return fail()
	}
	switch status {
	case Accepted, Finishing, Final:
		return status, nil
	default:
		return fail()
	}
}
