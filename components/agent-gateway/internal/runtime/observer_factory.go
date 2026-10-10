package runtime

import (
	"context"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// ReplacementObserverConfig is controller deployment material. It enables
// preparation only, not replacement admission, and carries no credentials.
type ReplacementObserverConfig struct {
	Scope          execution.ReplacementScope `json:"scope"`
	ClusterServer  string                     `json:"clusterServer"`
	DBInstanceID   string                     `json:"dbInstanceId"`
	LoadBalancerID string                     `json:"loadBalancerId"`
	ListenerID     string                     `json:"listenerId"`
	PrometheusURL  string                     `json:"prometheusUrl"`
	ClientTargets  []string                   `json:"clientTargets"`
	SQLBucket      string                     `json:"sqlBucket"`
	SQLKey         string                     `json:"sqlKey"`
}

func readProjectedObserverFile(name string, limit int64) ([]byte, error) {
	// Kubernetes projected secrets are symlinks; permit them only at this fixed
	// controller-owned mount. These paths never come from request or agent input.
	if name != "token" && name != "ca.crt" {
		return nil, ErrConfiguration
	}
	f, err := os.Open("/run/raptor/observer-api/" + name)
	if err != nil {
		return nil, ErrConfiguration
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() < 1 || stat.Size() > limit {
		return nil, ErrConfiguration
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, ErrConfiguration
	}
	return raw, nil
}
func BuildObservationPreparer(c LiveConfig, m Management) (ObservationPreparer, error) {
	return buildObservationPreparer(c, m, readProjectedObserverFile)
}
func buildObservationPreparer(c LiveConfig, m Management, mount func(string, int64) ([]byte, error)) (ObservationPreparer, error) {
	if c.ReplacementObserver == nil {
		return nil, nil
	}
	cfg := *c.ReplacementObserver
	scope := cfg.Scope
	binding := execution.AttemptBinding{Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", ObjectCode: scope.ProxyCode, EnvCode: scope.EnvCode, ClusterID: scope.Application.ClusterID}
	validID := regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	if c.AccountID != "1360282071200743" || c.Region != "ap-southeast-1" || scope.EnvCode != "rdev.ali" || scope.Application.Namespace != "raptor-test" || !scope.ValidFor(binding) || !regexp.MustCompile(`^asg-[A-Za-z0-9]+$`).MatchString(scope.GroupID) || !validID.MatchString(cfg.DBInstanceID) || !regexp.MustCompile(`^lsn-[A-Za-z0-9]+$`).MatchString(cfg.ListenerID) || !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`).MatchString(cfg.SQLBucket) || cfg.SQLKey == "" || strings.Contains(cfg.SQLKey, "..") || strings.HasPrefix(cfg.SQLKey, "/") {
		return nil, ErrConfiguration
	}
	issuer, err := m.ObserverIssuer(scope.ServerGroupID, cfg.LoadBalancerID)
	if err != nil {
		return nil, ErrConfiguration
	}
	ca, err := mount("ca.crt", 16384)
	if err != nil {
		return nil, ErrConfiguration
	}
	kube, err := NewObserverKubeIssuer(cfg.ClusterServer, scope.Application.Namespace, strings.TrimSuffix(scope.Application.Name, "-client")+"-observer", ca, func() (string, error) {
		raw, err := mount("token", 16384)
		if err != nil {
			return "", err
		}
		token := strings.TrimSpace(string(raw))
		if token == "" || strings.ContainsAny(token, "\r\n") {
			return "", ErrConfiguration
		}
		return token, nil
	})
	if err != nil {
		return nil, ErrConfiguration
	}
	host := cfg.SQLBucket + ".oss-ap-southeast-1.aliyuncs.com"
	client, err := oss.New("https://oss-ap-southeast-1.aliyuncs.com", "", "", oss.SetCredentialsProvider(ossCredentialProvider{source: m.credential}), oss.HTTPClient(&http.Client{Transport: fixedOSS{host: host}, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}))
	if err != nil {
		return nil, ErrConfiguration
	}
	bucket, err := client.Bucket(cfg.SQLBucket)
	if err != nil {
		return nil, ErrConfiguration
	}
	sql := ObserverSQLSource{Objects: OSSObjects{Client: client, Bucket: bucket, BucketName: cfg.SQLBucket}, Key: cfg.SQLKey, EnvCode: scope.EnvCode, DBCode: scope.TargetDBCode, DBInstanceID: cfg.DBInstanceID}
	preparation := ObserverPreparation{Server: cfg.ClusterServer, CA: ca, DBInstanceID: cfg.DBInstanceID, ListenerID: cfg.ListenerID, PrometheusURL: cfg.PrometheusURL, ClientTargets: append([]string(nil), cfg.ClientTargets...), Cloud: func(ctx context.Context, in execution.LiveStart) (ObserverCredential, error) {
		return issuer.Issue(ctx, scope.GroupID, in.Binding.AttemptID)
	}, Kube: func(ctx context.Context, in execution.LiveStart) (ObserverKubeCredential, error) {
		return kube.Issue(ctx)
	}, SQL: sql.Load}
	return func(ctx context.Context, in execution.LiveStart) (ObservationMaterial, error) {
		if in.Access.ReplacementScope == nil || *in.Access.ReplacementScope != scope {
			return ObservationMaterial{}, ErrConfiguration
		}
		return preparation.Prepare(ctx, in)
	}, nil
}
