package control

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestIdentityAndDuplicates(t *testing.T) {
	for _, ids := range [][2]string{{"", "p"}, {"u", ""}} {
		if _, err := New(ids[0], ids[1]); err == nil {
			t.Fatal("empty identity accepted")
		}
	}
	c, _ := New("u", "p")
	for _, ids := range [][2]string{{"wrong", "p"}, {"u", "wrong"}, {"", "p"}} {
		if _, err := c.finish(ids[0], ids[1]); err == nil {
			t.Fatal("mismatch accepted")
		}
	}
	select {
	case <-c.Done():
		t.Fatal("mismatch changed state")
	default:
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, e := c.finish("u", "p")
			if e != nil || (s != Accepted && s != Finishing) {
				t.Errorf("%s %v", s, e)
			}
		}()
	}
	wg.Wait()
	<-c.Done()
	c.MarkFinal()
	if s, _ := c.finish("u", "p"); s != Final {
		t.Fatal(s)
	}
}
func TestSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s")
	c, _ := New("u", "p")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, path, c) }()
	deadline := time.Now().Add(time.Second)
	for {
		if fi, err := os.Stat(path); err == nil && fi.Mode().Perm() == 0600 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no socket")
		}
		time.Sleep(time.Millisecond)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0600 {
		t.Fatal(fi.Mode())
	}
	if _, err := Request(ctx, path, "wrong", "p"); err == nil {
		t.Fatal("mismatch accepted")
	}
	if s, err := Request(ctx, path, "u", "p"); err != nil || s != Accepted {
		t.Fatal(s, err)
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := Request(ctx, path, "u", "p"); err == nil {
		t.Fatal("unsafe mode accepted")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestUnknownResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	os.Chmod(path, 0600)
	go func() { c, _ := l.Accept(); defer c.Close(); c.Write([]byte("\"unknown\"\n")) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Request(ctx, path, "u", "p"); err == nil {
		t.Fatal("unknown acknowledgement accepted")
	}
}

func TestMalformedRequestDoesNotFinish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s")
	c, _ := New("u", "p")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, path, c) }()
	for deadline := time.Now().Add(time.Second); ; {
		fi, err := os.Stat(path)
		if err == nil && fi.Mode().Perm() == 0600 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("socket unavailable")
		}
		time.Sleep(time.Millisecond)
	}
	for _, raw := range []string{"not json\n", "{\"podUID\":\"u\",\"processID\":\"p\",\"extra\":true}\n", "{\"podUID\":\"u\",\"processID\":\"p\"} {}\n"} {
		conn, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(time.Second))
		conn.Write([]byte(raw))
		var status Status
		json.NewDecoder(conn).Decode(&status)
		conn.Close()
		if status != "" {
			t.Fatal("malformed accepted", status)
		}
	}
	select {
	case <-c.Done():
		t.Fatal("malformed request finished traffic")
	default:
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Request(ctx, path, "u", "p")
			if err != nil || (s != Accepted && s != Finishing) {
				t.Errorf("%s %v", s, err)
			}
		}()
	}
	wg.Wait()
	c.MarkFinal()
	if s, err := Request(ctx, path, "u", "p"); err != nil || s != Final {
		t.Fatal(s, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := Request(context.Background(), path, "u", "p"); err == nil {
		t.Fatal("absent socket accepted")
	}
}
