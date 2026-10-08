package install

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/config"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Paths struct{ Directory, Config, Certificate, PrivateKey, BackendCA string }

func Install(root string, b config.Bundle, uid, gid int, now time.Time) (Paths, error) {
	fail := func() (Paths, error) { return Paths{}, errors.New("installation_failed") }
	root = filepath.Clean(root)
	// Every existing ancestor must be a real directory, never a symlink.
	for p := root; ; p = filepath.Dir(p) {
		s, e := os.Lstat(p)
		if e == nil && (!s.IsDir() || s.Mode()&os.ModeSymlink != 0) {
			return fail()
		}
		if e != nil && !os.IsNotExist(e) {
			return fail()
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	var e error
	if b.ServerCertificate != "" {
		pair, e := tls.X509KeyPair([]byte(b.ServerCertificate), []byte(b.ServerPrivateKey))
		if e != nil || len(pair.Certificate) == 0 {
			return fail()
		}
		leaf, e := x509.ParseCertificate(pair.Certificate[0])
		if e != nil || now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) || leaf.VerifyHostname(b.TLSHostname) != nil {
			return fail()
		}
		for _, ca := range []string{b.ClientCA, b.BackendCA} {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM([]byte(ca)) {
				return fail()
			}
		}
	}
	if e = os.Mkdir(root, 0700); e != nil && !os.IsExist(e) {
		return fail()
	}
	s, e := os.Lstat(root)
	if e != nil || !s.IsDir() || s.Mode().Perm() != 0700 {
		return fail()
	}
	stat, ok := s.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return fail()
	}
	dir, e := os.MkdirTemp(root, "generation-")
	if e != nil {
		return fail()
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(dir)
		}
	}()
	p := Paths{Directory: dir, Config: filepath.Join(dir, "pgcat.toml"), Certificate: filepath.Join(dir, "server.crt"), PrivateKey: filepath.Join(dir, "server.key"), BackendCA: filepath.Join(dir, "backend-ca.pem")}
	content := fmt.Sprintf("[general]\nhost = \"0.0.0.0\"\nport = 6432\nenable_prometheus_exporter = true\nprometheus_exporter_port = 9930\nconnect_timeout = 5000\nshutdown_timeout = 60000\nserver_tls = true\nverify_server_certificate = true\ntls_certificate = %s\ntls_private_key = %s\nadmin_username = %s\nadmin_password = %s\n[pools.test]\npool_mode = \"transaction\"\n[pools.test.users.0]\nusername = %s\npassword = %s\npool_size = 16\n[pools.test.shards.0]\ndatabase = %s\nservers = [[%s, 5432, \"primary\"]]\n", quote(p.Certificate), quote(p.PrivateKey), quote(b.AdminUser), quote(b.AdminPassword), quote(b.AppUser), quote(b.AppPassword), quote(b.Database), quote(b.DBHost))
	if b.ServerCertificate == "" {
		lines := strings.Split(content, "\n")
		filtered := []string{}
		for _, line := range lines {
			if strings.HasPrefix(line, "tls_certificate =") || strings.HasPrefix(line, "tls_private_key =") {
				continue
			}
			line = strings.ReplaceAll(line, "server_tls = true", "server_tls = false")
			line = strings.ReplaceAll(line, "verify_server_certificate = true", "verify_server_certificate = false")
			filtered = append(filtered, line)
		}
		content = strings.Join(filtered, "\n")
	}
	for path, value := range map[string]string{p.Config: content, p.Certificate: b.ServerCertificate, p.PrivateKey: b.ServerPrivateKey, p.BackendCA: b.BackendCA, filepath.Join(dir, "client-ca.pem"): b.ClientCA} {
		if value == "" {
			continue
		}
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return fail()
		}
		_, e = f.WriteString(value)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil || closeErr != nil {
			return fail()
		}
		if e = os.Chown(path, uid, gid); e != nil {
			return fail()
		}
	}
	if e = os.Chown(dir, uid, gid); e != nil {
		return fail()
	}
	success = true
	return p, nil
}
