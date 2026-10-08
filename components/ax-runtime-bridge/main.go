package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"github.com/google/ax/internal/substrate"
	ax "github.com/google/ax/pkg/apis/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 || r.TLS.PeerCertificates[0].Subject.CommonName != "raptor-gateway" {
		http.Error(w, "Unauthorized", 403)
		return
	}
	if r.Method != "POST" || r.URL.Path != "/v1/runtime" {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 23*1024*1024)
	defer r.Body.Close()
	var req Request
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&req) != nil || d.Decode(&extra) != io.EOF {
		http.Error(w, "InvalidRuntimeRequest", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	var out Response
	var e error
	switch req.Action {
	case "ensure":
		out, e = s.Ensure(ctx, req)
	case "start":
		out, e = s.Start(ctx, req)
	case "poll":
		out, e = s.Poll(ctx, req)
	case "read", "write":
		out, e = s.File(ctx, req)
	case "remove":
		out, e = s.Remove(ctx, req)
	default:
		e = ErrInvalid
	}
	code := 200
	if e != nil {
		out = Response{Error: "OutcomeUnconfirmed"}
		code = 503
		switch {
		case errors.Is(e, ErrInvalid):
			out.Error = "InvalidRuntimeRequest"
			code = 400
		case errors.Is(e, ErrConflict):
			out.Error = "OwnershipConflict"
			code = 409
		case errors.Is(e, ErrAbsent):
			out.Error = "ResourceAbsent"
			code = 404
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(out)
}
func main() {
	ca, e := os.ReadFile("/tls/ca.crt")
	if e != nil {
		log.Fatal("TLS configuration unavailable")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		log.Fatal("TLS CA invalid")
	}
	sub, e := substrate.NewClientWithOptions(substrate.ClientOptions{})
	if e != nil {
		log.Fatal("Substrate configuration unavailable")
	}
	defer sub.Close()
	conn, e := grpc.NewClient("ax-server.ax-system.svc:8080", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if e != nil {
		log.Fatal("AX connection unavailable")
	}
	defer conn.Close()
	image := os.Getenv("AX_PI_IMAGE")
	if image == "" {
		log.Fatal("fixed Pi image required")
	}
	var hosts []string
	if json.Unmarshal([]byte(os.Getenv("RUNTIME_EGRESS_HOSTS")), &hosts) != nil || len(hosts) == 0 {
		log.Fatal("runtime egress allowlist required")
	}
	s := &Service{StateDir: "/state", Image: image, Backend: &SDKBackend{AX: ax.NewAXClient(conn), Substrate: sub, Image: image, Atespace: "raptor-runtime", Router: "atenet-router.ate-system.svc:80", EgressHosts: hosts}}
	server := &http.Server{Addr: ":8443", Handler: s, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 130 * time.Second, IdleTimeout: 30 * time.Second}
	log.Fatal(server.ListenAndServeTLS("/tls/tls.crt", "/tls/tls.key"))
}
