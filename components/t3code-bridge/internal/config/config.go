package config

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var ErrConfig = errors.New("InvalidPrivateConfiguration")

type Config struct {
	Origin            string `json:"origin"`
	ApplicationCode   string `json:"applicationCode"`
	EnvironmentCode   string `json:"environmentCode"`
	Model             string `json:"model"`
	CredentialsFile   string `json:"credentialsFile"`
	StateDir          string `json:"stateDir"`
	AllowLoopbackHTTP bool   `json:"allowLoopbackHTTP"`
}

func (c Config) Validate() error {
	u, e := url.Parse(c.Origin)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
		return ErrConfig
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !c.AllowLoopbackHTTP || !(u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())) {
			return ErrConfig
		}
	}
	for _, s := range []string{c.ApplicationCode, c.EnvironmentCode} {
		if s == "" || len(s) > 128 || strings.ContainsAny(s, "/\\\x00\r\n") {
			return ErrConfig
		}
	}
	if c.Model != "gpt-5.6-luna" || !filepath.IsAbs(c.CredentialsFile) || !filepath.IsAbs(c.StateDir) {
		return ErrConfig
	}
	return nil
}
func PrivateRead(p string) ([]byte, error) {
	if !filepath.IsAbs(p) {
		return nil, ErrConfig
	}
	fd, e := syscall.Open(p, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return nil, ErrConfig
	}
	f := os.NewFile(uintptr(fd), p)
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 1<<20 {
		return nil, ErrConfig
	}
	b, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil || len(b) > 1<<20 {
		return nil, ErrConfig
	}
	return b, nil
}
func Load(p string) (Config, error) {
	var c Config
	b, e := PrivateRead(p)
	if e != nil {
		return c, e
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF {
		return c, ErrConfig
	}
	return c, c.Validate()
}
