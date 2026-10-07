provider "alicloud" { region = var.region }

data "alicloud_account" "current" {}
resource "terraform_data" "identity_refresh" {
  input = timestamp()
}
data "alicloud_account" "at_apply" {
  depends_on = [terraform_data.identity_refresh]
}
resource "terraform_data" "account_guard" {
  lifecycle {
    precondition {
      condition     = data.alicloud_account.current.id == var.account_id
      error_message = "Authenticated account does not match the selected account."
    }
    precondition {
      condition     = data.alicloud_account.at_apply.id == var.account_id
      error_message = "Apply-time account does not match the selected account."
    }
  }
}

# This adds one route only: no FC function, API group, RAM role/policy or
# Kubernetes RBAC resource is created or widened by this module.
resource "alicloud_api_gateway_api" "restart" {
  depends_on  = [terraform_data.account_guard]
  group_id    = var.group_id
  name        = "${var.name}-restart"
  description = "Basic Auth protected individual Deployment restart"
  auth_type   = "ANONYMOUS"
  request_config {
    protocol = "HTTPS"
    method   = "POST"
    path     = "/v1/deployment-restart"
    mode     = "PASSTHROUGH"
  }
  service_type = "FunctionCompute"
  fc_service_config {
    content_type_category = "CLIENT"
    function_version      = "3.0"
    function_type         = "HttpTrigger"
    region                = var.region
    function_name         = var.function_name
    function_base_url     = var.trigger_url
    path                  = "/v1/deployment-restart"
    method                = "POST"
    only_business_path    = true
    arn_role              = var.invoke_role_arn
    timeout               = 30000
  }
  stage_names = ["RELEASE"]
}
