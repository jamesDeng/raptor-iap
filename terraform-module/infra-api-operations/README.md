# Infra API restart route

Adds one HTTPS POST `/v1/deployment-restart` to an existing API Gateway group and existing protected FC3 function. The function authenticates forwarded Basic credentials. API Gateway `auth_type = "ANONYMOUS"` is intentional for that existing service-auth pattern; it does not make the FC trigger public. Backend invocation uses the supplied existing invocation role. Caller JSON Content-Type and exact trigger URL are preserved.

This module creates no FC function, gateway/group, RAM grant or Kubernetes grant. It does not alter the read module or expose proxy mutations. Singapore, current account, apply-time account, HTTPS URL and invocation-role account checks are included. The supplied role must already be restricted to the supplied function; account matching alone does not prove its effective policy. Central integration must inspect actual permissions and target identities.

Inputs: account_id, name, group_id, function_name, trigger_url and invoke_role_arn; region is fixed to Singapore. No credentials or private state are supplied through this public module. Output: restart_api_id. Terraform 1.13.3 and aliyun/alicloud 1.293.0 are pinned.

Offline checks: `terraform init -backend=false`, `terraform validate`, `terraform test` and `terraform fmt -check -recursive`. Tests use a mocked provider and synthetic identities; they do not establish deployed ingress behavior or permission acceptance.

Function configuration: the separate `components/infra-api/s.rdev-restart.yaml` is an explicit opt-in configuration for the existing function, with `INFRA_ENABLE_RESTART=true`. It is not automatically deployed. Default `s.rdev.yaml` remains unchanged. Before using the configuration, central coordination must review the function identity, secrets/profile references, selected target's Kubernetes grant and ingress, and apply the reviewed package at the chosen commit.

The private acceptance target, state/backend composition and all cloud application stay with central coordination. Do not initialize a backend from another session, copy its tfvars, or apply this module against shared state from this checkout.
