data "alicloud_account" "current" {}
# timestamp() remains unknown while planning, ensuring a fresh identity read
# when a saved plan executes with its current provider credentials.
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
      error_message = "Authenticated account does not match the requested account."
    }
    precondition {
      condition     = data.alicloud_account.at_apply.id == var.account_id
      error_message = "Apply-time authenticated account does not match the requested account."
    }
  }
}
provider "alicloud" { region = var.region }
locals {
  read_statements = concat(var.ack_only ? [
    { Effect = "Allow", Action = ["sts:GetCallerIdentity"], Resource = ["*"] }
    ] : [
    { Effect = "Allow", Action = ["sts:GetCallerIdentity", "rds:DescribeDBInstances", "rds:DescribeTags"], Resource = ["*"] },
    { Effect = "Allow", Action = ["ess:DescribeScalingGroups"], Resource = ["acs:ess:${var.region}:${var.account_id}:scalinggroup/*"] }
    ], var.cluster_id == "" ? [] : [
    { Effect = "Allow", Action = ["cs:DescribeClusterUserKubeconfig"], Resource = ["acs:cs:${var.region}:${var.account_id}:cluster/${var.cluster_id}"] }
  ])
  routes = { identity = "/v1/cloud/identity", deployments = "/v1/deployments", status = "/v1/deployment-status" }
}
resource "alicloud_ram_role" "read" {
  depends_on                  = [terraform_data.account_guard]
  role_name                   = "${var.name}-read"
  assume_role_policy_document = jsonencode({ Version = "1", Statement = [{ Effect = "Allow", Action = ["sts:AssumeRole"], Principal = { Service = ["fc.aliyuncs.com"] } }] })
  description                 = "Infra API read-only function role"
}
resource "alicloud_ram_policy" "read" {
  depends_on      = [terraform_data.account_guard]
  policy_name     = "${var.name}-read"
  policy_document = jsonencode({ Version = "1", Statement = local.read_statements })
}
resource "alicloud_ram_role_policy_attachment" "read" {
  role_name   = alicloud_ram_role.read.role_name
  policy_name = alicloud_ram_policy.read.policy_name
  policy_type = "Custom"
}
resource "alicloud_ram_role" "invoke" {
  depends_on                  = [terraform_data.account_guard]
  role_name                   = "${var.name}-invoke"
  assume_role_policy_document = jsonencode({ Version = "1", Statement = [{ Effect = "Allow", Action = ["sts:AssumeRole"], Principal = { Service = ["apigateway.aliyuncs.com"] } }] })
  description                 = "Gateway invocation of one protected FC3 function"
}
resource "alicloud_ram_policy" "invoke" {
  depends_on      = [terraform_data.account_guard]
  policy_name     = "${var.name}-invoke"
  policy_document = jsonencode({ Version = "1", Statement = [{ Effect = "Allow", Action = ["fc:InvokeFunction"], Resource = ["acs:fc:${var.region}:${var.account_id}:functions/${var.function_name}", "acs:fc:${var.region}:${var.account_id}:functions/${var.function_name}/*"] }] })
}
resource "alicloud_ram_role_policy_attachment" "invoke" {
  role_name   = alicloud_ram_role.invoke.role_name
  policy_name = alicloud_ram_policy.invoke.policy_name
  policy_type = "Custom"
}
resource "alicloud_api_gateway_group" "read" {
  instance_id = var.gateway_instance_id
  depends_on  = [terraform_data.account_guard]
  name        = var.name
  description = "Authenticated Infra API read service"
}
resource "alicloud_api_gateway_api" "read" {
  for_each    = local.routes
  group_id    = alicloud_api_gateway_group.read.id
  name        = "${var.name}-${each.key}"
  description = "Read-only ${each.key}"
  auth_type   = "ANONYMOUS"
  request_config {
    protocol = "HTTPS"
    method   = "GET"
    path     = each.value
    mode     = "PASSTHROUGH"
  }
  service_type = "FunctionCompute"
  fc_service_config {
    function_version   = "3.0"
    function_type      = "HttpTrigger"
    region             = var.region
    function_name      = var.function_name
    function_base_url  = var.trigger_url
    path               = each.value
    method             = "GET"
    only_business_path = true
    arn_role           = alicloud_ram_role.invoke.arn
    timeout            = 30000
  }
  # PASSTHROUGH forwards the caller header without a mapping declaration.
  stage_names = ["RELEASE"]
  depends_on  = [alicloud_ram_role_policy_attachment.invoke]
}

# MCP uses stateless JSON POST, retaining the existing protected FC backend.
resource "alicloud_api_gateway_api" "mcp" {
  group_id    = alicloud_api_gateway_group.read.id
  name        = "${var.name}-mcp"
  description = "Authenticated read-only MCP"
  auth_type   = "ANONYMOUS"
  request_config {
    protocol = "HTTPS"
    method   = "POST"
    path     = "/mcp"
    mode     = "PASSTHROUGH"
  }
  service_type = "FunctionCompute"
  fc_service_config {
    function_version   = "3.0"
    function_type      = "HttpTrigger"
    region             = var.region
    function_name      = var.function_name
    function_base_url  = var.trigger_url
    path               = "/mcp"
    method             = "POST"
    only_business_path = true
    arn_role           = alicloud_ram_role.invoke.arn
    timeout            = 30000
  }
  stage_names = ["RELEASE"]
  depends_on  = [alicloud_ram_role_policy_attachment.invoke]
}
