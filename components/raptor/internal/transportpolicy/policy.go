package transportpolicy

import "net/url"

// Allowed keeps remote HTTP disabled unless an operator opts into the exact private cluster origin.
func Allowed(raw, clusterOrigin string, allowCluster bool) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	if u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1" {
		return true
	}
	return allowCluster && raw == clusterOrigin && (clusterOrigin == "http://agent-gateway:8874" || clusterOrigin == "http://raptor-backend:8871")
}
