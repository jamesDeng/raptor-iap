package main

import (
	"reflect"
	"regexp"
	"strings"
)

const liveConfigPath = "/run/raptor/live/config.json"

func named(list any, name string) map[string]any {
	values, _ := list.([]any)
	var found map[string]any
	for _, v := range values {
		m := obj(v)
		if m["name"] == name {
			if found != nil {
				return nil
			}
			found = m
		}
	}
	return found
}
func exactEnv(c map[string]any, name, want string) bool {
	e := named(c["env"], name)
	return e != nil && e["value"] == want && e["valueFrom"] == nil
}
func sequence(v any, want ...string) bool {
	a, ok := v.([]any)
	if !ok || len(a) != len(want) {
		return false
	}
	for i, s := range want {
		if a[i] != s {
			return false
		}
	}
	return true
}
func overlaps(path, other string) bool {
	return path == other || strings.HasPrefix(path, other+"/") || strings.HasPrefix(other, path+"/") || other == "/"
}
func mount(c map[string]any, name, path string, readOnly bool) bool {
	mounts, _ := c["volumeMounts"].([]any)
	count := 0
	for _, v := range mounts {
		m := obj(v)
		if m["name"] == name {
			count++
			if m["mountPath"] != path || m["subPath"] != nil || m["subPathExpr"] != nil || m["mountPropagation"] != nil || (m["readOnly"] == true) != readOnly {
				return false
			}
		} else if overlaps(path, str(m["mountPath"])) {
			return false
		}
	}
	return count == 1
}
func copiesConfig(c map[string]any) bool {
	if c["env"] != nil || c["envFrom"] != nil || !sequence(c["command"], "/bin/sh", "-ec") {
		return false
	}
	args, ok := c["args"].([]any)
	if !ok || len(args) != 1 {
		return false
	}
	base := "umask 077; install -m 0600 /live-config-source/config.json /live-private/config.json"
	sts := "; install -m 0600 /live-controller-source/credential.json /live-private/credential.json"
	tls := "; install -m 0600 /ax-tls-source/ca.crt /live-private/ax-ca.crt; install -m 0600 /ax-tls-source/tls.crt /live-private/ax-client.crt; install -m 0600 /ax-tls-source/tls.key /live-private/ax-client.key"
	// Exact Helm-supported scripts; arbitrary shell prefixes/suffixes are rejected.
	return args[0] == base || args[0] == base+sts || args[0] == base+tls || args[0] == base+sts+tls
}
func bindingSpec(spec map[string]any) (string, bool) {
	gateway := named(spec["containers"], deployment)
	init := named(spec["initContainers"], "live-private-files")
	image := str(gateway["image"])
	if !regexp.MustCompile(`^ghcr\.io/jamesdeng/raptor-iap-gateway@sha256:[a-f0-9]{64}$`).MatchString(image) || init["image"] != image {
		return "", false
	}
	if !sequence(gateway["command"], "/usr/local/bin/gateway") || gateway["args"] != nil && !sequence(gateway["args"]) {
		return "", false
	}
	if !exactEnv(gateway, "GATEWAY_RUNTIME_MODE", "live") || !exactEnv(gateway, "GATEWAY_LIVE_CONFIG_FILE", liveConfigPath) || !copiesConfig(init) {
		return "", false
	}
	if !mount(gateway, "live-private", "/run/raptor/live", true) || !mount(init, "live-private", "/live-private", false) || !mount(init, "live-config-source", "/live-config-source", true) {
		return "", false
	}
	private := named(spec["volumes"], "live-private")
	source := named(spec["volumes"], "live-config-source")
	if !allowed(private, "name emptyDir") || obj(private["emptyDir"])["medium"] != "Memory" || !allowed(source, "name secret") {
		return "", false
	}
	secret := obj(source["secret"])
	if secret["secretName"] != secretName || secret["optional"] == true {
		return "", false
	}
	if items, exists := secret["items"]; exists {
		a, ok := items.([]any)
		if !ok || len(a) != 1 || obj(a[0])["key"] != "config.json" || obj(a[0])["path"] != "config.json" {
			return "", false
		}
	}
	// No other init, sidecar or ephemeral container may change the copied bytes.
	for _, kind := range []string{"containers", "initContainers", "ephemeralContainers"} {
		containers, _ := spec[kind].([]any)
		for _, v := range containers {
			c := obj(v)
			if kind == "containers" && c["name"] == deployment || kind == "initContainers" && c["name"] == "live-private-files" {
				continue
			}
			mounts, _ := c["volumeMounts"].([]any)
			for _, m := range mounts {
				if obj(m)["name"] == "live-private" {
					return "", false
				}
			}
		}
	}
	return image, true
}
func imageMatches(status map[string]any, image string) bool {
	digest := strings.Split(image, "@")[1]
	id := str(status["imageID"])
	return id == image || id == "docker-pullable://"+image || id == digest || id == "containerd://"+digest
}

// loadedConfigBinding establishes that the immutable bytes read via exec are
// the path used by the running Gateway, copied by its successfully completed
// init container from the intended Secret and inaccessible to other writers.
func loadedConfigBinding(d, p map[string]any) bool {
	expected, ok := bindingSpec(obj(obj(obj(d["spec"])["template"])["spec"]))
	if !ok {
		return false
	}
	actual, ok := bindingSpec(obj(p["spec"]))
	if !ok || actual != expected {
		return false
	}
	ds := obj(obj(obj(d["spec"])["template"])["spec"])
	ps := obj(p["spec"])
	// Both configurations must use the same accepted init copy script.
	if !reflect.DeepEqual(named(ds["initContainers"], "live-private-files")["args"], named(ps["initContainers"], "live-private-files")["args"]) {
		return false
	}
	status := obj(p["status"])
	gateway := named(status["containerStatuses"], deployment)
	init := named(status["initContainerStatuses"], "live-private-files")
	return gateway["ready"] == true && obj(obj(gateway["state"])["running"]) != nil && imageMatches(gateway, expected) && number(obj(obj(init["state"])["terminated"])["exitCode"]) == 0 && imageMatches(init, expected)
}
