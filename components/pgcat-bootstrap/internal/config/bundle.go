package config

import "errors"

type Bundle struct {
	SchemaVersion     int    `json:"schema_version"`
	Env               string `json:"env"`
	ProxyCode         string `json:"proxy_code"`
	TargetDBCode      string `json:"target_db_code"`
	DBHost            string `json:"db_host"`
	Database          string `json:"database"`
	TLSHostname       string `json:"tls_hostname"`
	AppUser           string `json:"app_user"`
	AppPassword       string `json:"app_password"`
	AdminUser         string `json:"admin_user"`
	AdminPassword     string `json:"admin_password"`
	ServerCertificate string `json:"server_certificate"`
	ServerPrivateKey  string `json:"server_private_key"`
	ClientCA          string `json:"client_ca"`
	BackendCA         string `json:"backend_ca"`
}

func ParseBundle(data []byte, m Metadata) (Bundle, error) {
	var b Bundle
	if e := decode(data, &b); e != nil {
		return b, e
	}
	if b.SchemaVersion != 1 || b.Env != m.Env || b.ProxyCode != m.ProxyCode || b.TargetDBCode != m.TargetDBCode || b.DBHost != m.DBHost || b.Database != m.Database {
		return Bundle{}, errors.New("bundle_identity_mismatch")
	}
	for _, s := range []string{b.AppUser, b.AppPassword, b.AdminUser, b.AdminPassword} {
		if len(s) == 0 || len(s) > 64*1024 {
			return Bundle{}, errors.New("invalid_bundle_field")
		}
	}
	return b, nil
}
