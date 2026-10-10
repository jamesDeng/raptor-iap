package catalog

import (
	"context"
	"encoding/json"
	"net/url"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

var columnKeys = map[string]bool{"cloud": true, "region": true, "accountId": true, "ackClusterId": true, "clusterId": true, "namespace": true, "infraApiUrl": true}
var repoKeys = map[string]bool{"terraformRepo": true, "terraformBaseBranch": true, "terraformPath": true, "kubernetesRepo": true, "kubernetesBaseBranch": true, "kubernetesPath": true}

func configString(c map[string]any, key string) string { v, _ := c[key].(string); return v }
func validRepository(r domain.EnvironmentRepository) bool {
	if r.Purpose != "terraform" && r.Purpose != "kubernetes" {
		return false
	}
	if !validRepositoryURL(r.URL) {
		return false
	}
	if strings.TrimSpace(r.BaseBranch) == "" || strings.ContainsAny(r.BaseBranch, " \t\n~^:?*[\\") || strings.HasPrefix(r.BaseBranch, "-") {
		return false
	}
	if r.Directory == "" || path.IsAbs(r.Directory) || path.Clean(r.Directory) != r.Directory || r.Directory == ".." || strings.HasPrefix(r.Directory, "../") || strings.Contains(r.Directory, "\\") {
		return false
	}
	return true
}
func validRepositoryURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(strings.Split(strings.Trim(u.Path, "/"), "/")) != 2 {
		return false
	}
	return true
}
func repoFromConfig(c map[string]any, purpose string) (domain.EnvironmentRepository, bool) {
	prefix := purpose
	if purpose == "kubernetes" && configString(c, "kubernetesRepo") == "" {
		prefix = "k8s"
	}
	r := domain.EnvironmentRepository{Purpose: purpose, URL: configString(c, prefix+"Repo"), BaseBranch: configString(c, prefix+"BaseBranch"), Directory: configString(c, prefix+"Path")}
	return r, r.URL != ""
}
func (s *Service) saveStructuredEnvironment(ctx context.Context, v domain.Environment, update bool) error {
	if !codePattern.MatchString(v.Code) || !validName(v.Stage) || v.GroupCode == "" || (v.Config != nil && !validConfig(v.Config)) {
		return domain.ErrInvalid
	}
	for _, purpose := range []string{"terraform", "kubernetes", "k8s"} {
		if raw, ok := v.Config[purpose+"Repo"]; ok {
			urlString, ok := raw.(string)
			if !ok || (urlString != "" && !validRepositoryURL(urlString)) {
				return domain.ErrInvalid
			}
		}
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return storeError(e)
	}
	defer tx.Rollback(ctx)
	aux := v.Config
	var previousRepos []domain.EnvironmentRepository
	if update {
		old, e := loadEnvironment(ctx, tx, v.Code, true)
		if e != nil {
			return e
		}
		previousRepos = old.Repositories
		if v.Config != nil {
			for k, value := range v.Config {
				old.Config[k] = value
			}
			v.Config = old.Config
			aux = old.Config
			v.Cloud = configString(old.Config, "cloud")
			v.Region = configString(old.Config, "region")
			v.AccountID = configString(old.Config, "accountId")
			if v.AccountID == "" {
				v.AccountID = configString(old.Config, "cloudAccountId")
			}
			v.ACKClusterID = configString(old.Config, "ackClusterId")
			v.Repositories = old.Repositories
			for _, purpose := range []string{"terraform", "kubernetes"} {
				if r, ok := repoFromConfig(old.Config, purpose); ok {
					found := false
					for i := range v.Repositories {
						if v.Repositories[i].Purpose == purpose {
							v.Repositories[i] = r
							found = true
							break
						}
					}
					if !found {
						v.Repositories = append(v.Repositories, r)
					}
				}
			}
		} else {
			aux = old.Config
		}
	} else if v.Config != nil {
		if v.Cloud == "" {
			v.Cloud = configString(v.Config, "cloud")
		}
		if v.Region == "" {
			v.Region = configString(v.Config, "region")
		}
		if v.AccountID == "" {
			v.AccountID = configString(v.Config, "accountId")
			if v.AccountID == "" {
				v.AccountID = configString(v.Config, "cloudAccountId")
			}
		}
		if v.ACKClusterID == "" {
			v.ACKClusterID = configString(v.Config, "ackClusterId")
		}
		for _, purpose := range []string{"terraform", "kubernetes"} {
			if r, ok := repoFromConfig(v.Config, purpose); ok {
				v.Repositories = append(v.Repositories, r)
			}
		}
	}
	// Existing HTTP clients may carry repository URLs without a base branch. Preserve those records;
	// new structured submissions must provide complete repository metadata.
	if v.Config == nil {
		seen := map[string]bool{}
		for _, r := range v.Repositories {
			unchanged := false
			for _, old := range previousRepos {
				if old == r {
					unchanged = true
					break
				}
			}
			identity := r.Purpose + "\x00" + r.URL + "\x00" + r.Directory
			if seen[identity] || (!unchanged && !validRepository(r)) {
				return domain.ErrInvalid
			}
			seen[identity] = true
		}
	}
	if !validConfig(v.Config) {
		return domain.ErrInvalid
	}
	if update {
		tag, e := tx.Exec(ctx, "UPDATE raptor.environments SET group_code=$2,stage=$3,cloud=$4,region=$5,account_id=$6,ack_cluster_id=$7,cluster_id=$8,namespace=$9,infra_api_url=$10 WHERE code=$1", v.Code, v.GroupCode, v.Stage, v.Cloud, v.Region, v.AccountID, v.ACKClusterID, configString(aux, "clusterId"), configString(aux, "namespace"), configString(aux, "infraApiUrl"))
		if e != nil {
			return storeError(e)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
	} else {
		_, e := tx.Exec(ctx, "INSERT INTO raptor.environments(code,group_code,stage,cloud,region,account_id,ack_cluster_id,cluster_id,namespace,infra_api_url) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", v.Code, v.GroupCode, v.Stage, v.Cloud, v.Region, v.AccountID, v.ACKClusterID, configString(v.Config, "clusterId"), configString(v.Config, "namespace"), configString(v.Config, "infraApiUrl"))
		if e != nil {
			return storeError(e)
		}
	}
	if v.Config != nil {
		for k, value := range v.Config {
			if columnKeys[k] {
				if _, ok := value.(string); ok {
					continue
				}
			}
			if repoKeys[k] {
				if _, ok := value.(string); ok && (k == "terraformRepo" || k == "kubernetesRepo" || configString(v.Config, strings.TrimSuffix(strings.TrimSuffix(k, "Path"), "BaseBranch")+"Repo") != "") {
					continue
				}
			}
			b, e := json.Marshal(value)
			if e != nil || len(b) > 16384 {
				return domain.ErrInvalid
			}
			_, e = tx.Exec(ctx, "INSERT INTO raptor.environment_settings(environment_code,key,value_json) VALUES($1,$2,$3) ON CONFLICT(environment_code,key) DO UPDATE SET value_json=EXCLUDED.value_json", v.Code, k, string(b))
			if e != nil {
				return storeError(e)
			}
		}
	}
	if v.Repositories != nil {
		if _, e = tx.Exec(ctx, "DELETE FROM raptor.environment_repositories WHERE environment_code=$1", v.Code); e != nil {
			return storeError(e)
		}
		for _, r := range v.Repositories {
			if _, e = tx.Exec(ctx, "INSERT INTO raptor.environment_repositories(environment_code,purpose,repository_url,base_branch,directory) VALUES($1,$2,$3,$4,$5)", v.Code, r.Purpose, r.URL, r.BaseBranch, r.Directory); e != nil {
				return storeError(e)
			}
		}
	}
	return storeError(tx.Commit(ctx))
}

func loadEnvironment(ctx context.Context, tx pgx.Tx, code string, lock bool) (domain.Environment, error) {
	var v domain.Environment
	var cluster, namespace, infra string
	query := "SELECT code,group_code,stage,cloud,region,account_id,ack_cluster_id,cluster_id,namespace,infra_api_url FROM raptor.environments WHERE code=$1"
	if lock {
		query += " FOR UPDATE"
	}
	if e := tx.QueryRow(ctx, query, code).Scan(&v.Code, &v.GroupCode, &v.Stage, &v.Cloud, &v.Region, &v.AccountID, &v.ACKClusterID, &cluster, &namespace, &infra); e != nil {
		return v, storeError(e)
	}
	v.Config = map[string]any{}
	rows, e := tx.Query(ctx, "SELECT key,value_json FROM raptor.environment_settings WHERE environment_code=$1", code)
	if e != nil {
		return v, storeError(e)
	}
	for rows.Next() {
		var k, raw string
		if e = rows.Scan(&k, &raw); e != nil {
			break
		}
		var value any
		if e = json.Unmarshal([]byte(raw), &value); e != nil {
			break
		}
		v.Config[k] = value
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return v, storeError(e)
	}
	for k, value := range map[string]string{"cloud": v.Cloud, "region": v.Region, "accountId": v.AccountID, "ackClusterId": v.ACKClusterID, "clusterId": cluster, "namespace": namespace, "infraApiUrl": infra} {
		if value != "" {
			v.Config[k] = value
		}
	}
	if _, legacy := v.Config["cloudAccountId"]; legacy && v.AccountID != "" {
		v.Config["cloudAccountId"] = v.AccountID
	}
	rows, e = tx.Query(ctx, "SELECT purpose,repository_url,base_branch,directory FROM raptor.environment_repositories WHERE environment_code=$1 ORDER BY purpose,id", code)
	if e != nil {
		return v, storeError(e)
	}
	v.Repositories = []domain.EnvironmentRepository{}
	for rows.Next() {
		var r domain.EnvironmentRepository
		if e = rows.Scan(&r.Purpose, &r.URL, &r.BaseBranch, &r.Directory); e != nil {
			break
		}
		v.Repositories = append(v.Repositories, r)
		_, exists := v.Config[r.Purpose+"Repo"]
		if !exists {
			v.Config[r.Purpose+"Repo"] = r.URL
			if r.BaseBranch != "" {
				v.Config[r.Purpose+"BaseBranch"] = r.BaseBranch
			}
			if r.Directory != "" {
				v.Config[r.Purpose+"Path"] = r.Directory
			}
		}
		if r.Purpose == "kubernetes" && !exists {
			if _, legacy := v.Config["k8sRepo"]; legacy {
				v.Config["k8sRepo"] = r.URL
			}
			if _, legacy := v.Config["k8sBaseBranch"]; legacy && r.BaseBranch != "" {
				v.Config["k8sBaseBranch"] = r.BaseBranch
			}
			if _, legacy := v.Config["k8sPath"]; legacy && r.Directory != "" {
				v.Config["k8sPath"] = r.Directory
			}
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return v, storeError(e)
	}
	if !validConfig(v.Config) {
		return v, domain.ErrInvalid
	}
	return v, nil
}
func (s *Service) getStructuredEnvironment(ctx context.Context, code string) (domain.Environment, error) {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return domain.Environment{}, storeError(e)
	}
	defer tx.Rollback(ctx)
	v, e := loadEnvironment(ctx, tx, code, false)
	if e != nil {
		return v, e
	}
	return v, storeError(tx.Commit(ctx))
}
func (s *Service) listStructuredEnvironments(ctx context.Context) ([]domain.Environment, error) {
	rows, e := s.Pool.Query(ctx, "SELECT code FROM raptor.environments ORDER BY group_code,code")
	if e != nil {
		return nil, storeError(e)
	}
	codes := []string{}
	for rows.Next() {
		var code string
		if e = rows.Scan(&code); e != nil {
			break
		}
		codes = append(codes, code)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return nil, storeError(e)
	}
	out := []domain.Environment{}
	for _, code := range codes {
		v, e := s.getStructuredEnvironment(ctx, code)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
