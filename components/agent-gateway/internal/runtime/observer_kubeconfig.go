package runtime

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

func validateObserverKubeconfig(raw []byte, b execution.AttemptBinding, s execution.ReplacementScope, until time.Time) error {
	var config struct {
		APIVersion     string `json:"apiVersion"`
		Kind           string `json:"kind"`
		CurrentContext string `json:"current-context"`
		Clusters       []struct {
			Name    string `json:"name"`
			Cluster struct {
				Server string `json:"server"`
				CA     string `json:"certificate-authority-data"`
			} `json:"cluster"`
		} `json:"clusters"`
		Contexts []struct {
			Name    string                                    `json:"name"`
			Context struct{ Cluster, User, Namespace string } `json:"context"`
		} `json:"contexts"`
		Users []struct {
			Name string `json:"name"`
			User struct {
				Token string `json:"token"`
			} `json:"user"`
		} `json:"users"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil {
		return ErrConfiguration
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return ErrConfiguration
	}
	if config.APIVersion != "v1" || config.Kind != "Config" || config.CurrentContext != "observer" || len(config.Clusters) != 1 || len(config.Contexts) != 1 || len(config.Users) != 1 {
		return ErrConfiguration
	}
	cluster, ctx, user := config.Clusters[0], config.Contexts[0], config.Users[0]
	if cluster.Name != b.ClusterID || ctx.Name != "observer" || ctx.Context.Cluster != b.ClusterID || ctx.Context.User != "observer" || ctx.Context.Namespace != s.Application.Namespace || user.Name != "observer" || len(user.User.Token) > 16384 {
		return ErrConfiguration
	}
	u, e := url.Parse(cluster.Cluster.Server)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !net.ParseIP(u.Hostname()).IsPrivate() {
		return ErrConfiguration
	}
	ca, e := base64.StdEncoding.DecodeString(cluster.Cluster.CA)
	if e != nil || len(ca) == 0 || len(ca) > 16384 {
		return ErrConfiguration
	}
	// Claims are an additional identity check, not signature verification. The
	// token must originate from the authenticated TLS TokenRequest issuer.
	parts := strings.Split(user.User.Token, ".")
	if len(parts) != 3 {
		return ErrConfiguration
	}
	payload, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return ErrConfiguration
	}
	var claims struct {
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ErrConfiguration
	}
	expected := "system:serviceaccount:" + s.Application.Namespace + ":" + strings.TrimSuffix(s.Application.Name, "-client") + "-observer"
	if claims.Sub != expected || time.Unix(claims.Exp, 0).Before(until) || claims.Exp < time.Now().Add(120*time.Second).Unix() || claims.Exp > time.Now().Add(930*time.Second).Unix() {
		return ErrConfiguration
	}
	return nil
}
