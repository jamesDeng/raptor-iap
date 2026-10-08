package install

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/config"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderEscapes(t *testing.T) {
	got := quote("pass\"\\\n\t")
	if got != `"pass\"\\\n\t"` {
		t.Fatalf("unsafe string: %s", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatal("literal newline")
	}
}

func TestInstallRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "link")
	if e := os.Symlink(root, link); e != nil {
		t.Fatal(e)
	}
	if _, e := Install(link, config.Bundle{}, os.Getuid(), os.Getgid(), time.Now()); e == nil {
		t.Fatal("accepted symlink")
	}
}
func TestInstallTLSAndPermissions(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"proxy.internal"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	keyder, _ := x509.MarshalPKCS8PrivateKey(key)
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	b := config.Bundle{TLSHostname: "proxy.internal", ServerCertificate: cert, ServerPrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyder})), ClientCA: cert, BackendCA: cert, DBHost: "db.internal", Database: "test", AppUser: "app", AppPassword: "synthetic", AdminUser: "admin", AdminPassword: "synthetic"}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "private")
	p, e := Install(root, b, os.Getuid(), os.Getgid(), now)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range []string{p.Config, p.Certificate, p.PrivateKey, p.BackendCA} {
		s, e := os.Stat(f)
		if e != nil || s.Mode().Perm() != 0600 {
			t.Fatalf("unsafe mode: %v", e)
		}
	}
	s, _ := os.Stat(root)
	if s.Mode().Perm() != 0700 {
		t.Fatal("unsafe directory")
	}
	b.TLSHostname = "wrong.internal"
	if _, e := Install(root, b, os.Getuid(), os.Getgid(), now); e == nil {
		t.Fatal("accepted hostname mismatch")
	}
	b.TLSHostname = "proxy.internal"
	if _, e := Install(root, b, os.Getuid(), os.Getgid(), now.Add(2*time.Hour)); e == nil {
		t.Fatal("accepted expired certificate")
	}
}

func TestPlaintextInstallation(t *testing.T) {
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	p, e := Install(filepath.Join(parent, "private"), config.Bundle{DBHost: "db.internal", Database: "test", AppUser: "app", AppPassword: "synthetic", AdminUser: "admin", AdminPassword: "synthetic"}, os.Getuid(), os.Getgid(), time.Now())
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(p.Config)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(b), "server_tls = false") || strings.Contains(string(b), "tls_certificate =") {
		t.Fatal("TLS still configured")
	}
}
