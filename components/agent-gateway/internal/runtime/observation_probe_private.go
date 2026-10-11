package runtime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"
)

func observerKubeRead(ctx context.Context, in ObserverProbeInput) (json.RawMessage, error) {
	c, e := observerConfigFrom(in)
	if e != nil || c.Application.Namespace != "raptor-test" {
		return nil, ErrConfiguration
	}
	prefix := []string{"--kubeconfig", "/tmp/raptor-private/kubeconfig", "--namespace", c.Application.Namespace, "get"}
	if len(in.Args) < 8 || !reflect.DeepEqual(in.Args[:5], prefix) || !reflect.DeepEqual(in.Args[len(in.Args)-2:], []string{"--output", "json"}) || len(in.Env) != 0 {
		return nil, ErrConfiguration
	}
	kind := in.Args[5]
	path := ""
	query := ""
	if kind == "deployment" && len(in.Args) == 9 && in.Args[6] == c.Application.Name {
		path = "/apis/apps/v1/namespaces/" + c.Application.Namespace + "/deployments/" + url.PathEscape(c.Application.Name)
	} else if (kind == "pods" || kind == "replicasets") && len(in.Args) == 10 && in.Args[6] == "--selector" && len(in.Args[7]) <= 1024 && regexp.MustCompile(`^[-A-Za-z0-9_./]+=[-A-Za-z0-9_.]+(,[-A-Za-z0-9_./]+=[-A-Za-z0-9_.]+)*$`).MatchString(in.Args[7]) {
		if kind == "pods" {
			path = "/api/v1/namespaces/" + c.Application.Namespace + "/pods"
		} else {
			path = "/apis/apps/v1/namespaces/" + c.Application.Namespace + "/replicasets"
		}
		query = "?labelSelector=" + url.QueryEscape(in.Args[7])
	} else {
		return nil, ErrConfiguration
	}
	var kube struct {
		Clusters []struct {
			Cluster struct {
				Server string
				CA     string `json:"certificate-authority-data"`
			}
		}
		Users []struct{ User struct{ Token string } }
	}
	if json.Unmarshal(in.Kubeconfig, &kube) != nil || len(kube.Clusters) != 1 || len(kube.Users) != 1 {
		return nil, ErrConfiguration
	}
	cluster := kube.Clusters[0].Cluster
	u, e := url.Parse(cluster.Server)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !net.ParseIP(u.Hostname()).IsPrivate() {
		return nil, ErrConfiguration
	}
	ca, e := base64.StdEncoding.DecodeString(cluster.CA)
	roots := x509.NewCertPool()
	if e != nil || !roots.AppendCertsFromPEM(ca) {
		return nil, ErrConfiguration
	}
	token := kube.Users[0].User.Token
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, ErrConfiguration
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, e := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(cluster.Server, "/")+path+query, nil)
	if e != nil {
		return nil, ErrConfiguration
	}
	r.Header.Set("Authorization", "Bearer "+token)
	response, e := client.Do(r)
	if e != nil {
		return nil, ErrRuntimeUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, ErrRuntimeUnavailable
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 262145))
	if e != nil || len(raw) > 262144 || !json.Valid(raw) {
		return nil, ErrRuntimeUnavailable
	}
	return raw, nil
}
func observerSQLRead(ctx context.Context, in ObserverProbeInput) (json.RawMessage, error) {
	fixed := []string{"--no-psqlrc", "--no-password", "--tuples-only", "--no-align", "--command", "SELECT 1"}
	c, e := observerConfigFrom(in)
	env := in.Env
	sql := c.Private.SQL
	if e != nil || !reflect.DeepEqual(in.Args, fixed) || len(env) != 8 || !net.ParseIP(env["PGHOST"]).IsPrivate() || env["PGPORT"] != "6432" || env["PGUSER"] != sql.User || env["PGPASSWORD"] != sql.Password || env["PGDATABASE"] != sql.Database || env["PGSSLMODE"] != sql.SSLMode || env["PGCONNECT_TIMEOUT"] != "5" || env["PGOPTIONS"] != "-c default_transaction_read_only=on -c statement_timeout=5000" || (sql.SSLMode != "disable" && sql.SSLMode != "verify-full") {
		return nil, ErrConfiguration
	}
	config, e := pgx.ParseConfig("host=127.0.0.1 sslmode=disable")
	if e != nil {
		return nil, ErrConfiguration
	}
	config.Host = env["PGHOST"]
	config.Port = 6432
	config.User = sql.User
	config.Password = sql.Password
	config.Database = sql.Database
	config.ConnectTimeout = 5 * time.Second
	config.RuntimeParams = map[string]string{"default_transaction_read_only": "on", "statement_timeout": "5000"}
	config.Fallbacks = nil
	if sql.SSLMode == "verify-full" {
		config.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.Host}
	}
	connection, e := pgx.ConnectConfig(ctx, config)
	if e != nil {
		return nil, ErrRuntimeUnavailable
	}
	defer connection.Close(context.Background())
	if observerSelectOne(ctx, connection) != nil {
		return nil, ErrRuntimeUnavailable
	}
	return json.RawMessage(`{"connected":true}`), nil
}

// A fixed literal query needs no named prepared statement; transaction poolers
// may not retain those statements across backend connections.
func observerSelectOne(ctx context.Context, connection *pgx.Conn) error {
	var one int
	if connection.QueryRow(ctx, "SELECT 1", pgx.QueryExecModeSimpleProtocol).Scan(&one) != nil || one != 1 {
		return ErrRuntimeUnavailable
	}
	return nil
}
